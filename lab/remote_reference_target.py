"""Bounded synthetic LAN HTTP target with independent server-side NDJSON ledger.

Use only on loopback or a selected RFC1918 LAN address, never the public Internet.
"""

import argparse
from datetime import datetime, timezone
import ipaddress
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import threading
import time
from urllib.parse import urlsplit

MARKER = b"damocline-remote-reference-v1\n"


def private_bind(value):
    try:
        address = ipaddress.IPv4Address(value)
    except ipaddress.AddressValueError as exc:
        raise argparse.ArgumentTypeError("--bind must be an explicit IPv4 address") from exc
    permitted = [ipaddress.ip_network(net) for net in
                 ("127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")]
    if not any(address in net for net in permitted):
        raise argparse.ArgumentTypeError("--bind must be loopback or RFC1918 IPv4 (not 0.0.0.0)")
    return str(address)


class ReferenceServer(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, address, log_path, delay_ms, max_inflight):
        # Refuse overwrite before binding a socket. Close resources on failure.
        self.ledger = open(log_path, "x", encoding="utf-8", buffering=1)
        try:
            super().__init__(address, ReferenceHandler)
        except BaseException:
            self.ledger.close()
            raise
        self.delay_seconds = delay_ms / 1000
        self.slots = threading.BoundedSemaphore(max_inflight)
        self.ledger_lock = threading.Lock()
        self.active = threading.Event()  # deterministic fixture tests only

    def server_close(self):
        super().server_close()
        self.ledger.close()


class ReferenceHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "DamoclineReference/1"

    def do_GET(self):
        route = urlsplit(self.path).path
        if route == "/healthz":
            self.send_response(200)
            self.send_header("Content-Length", "2")
            self.end_headers()
            self.wfile.write(b"ok")
            return
        if route != "/work" or self.path != "/work":
            self.send_error(404)
            return
        start = time.perf_counter()
        acquired = self.server.slots.acquire(blocking=False)
        status = 200 if acquired else 503
        try:
            if acquired:
                self.server.active.set()
                time.sleep(self.server.delay_seconds)
            body = MARKER if acquired else b"reference-target-busy\n"
            try:
                self.send_response(status)
                self.send_header("Content-Type", "text/plain; charset=utf-8")
                self.send_header("Cache-Control", "no-store")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
                pass
        finally:
            if acquired:
                self.server.slots.release()
            record = {
                "time": datetime.now(timezone.utc).isoformat(timespec="microseconds"),
                "method": "GET", "path": "/work", "status": status,
                "duration_ms": round((time.perf_counter() - start) * 1000, 3),
            }
            with self.server.ledger_lock:
                self.server.ledger.write(json.dumps(record, separators=(",", ":")) + "\n")

    def log_message(self, *_args):
        # Avoid recording client IPs, paths, or headers in terminal output.
        return


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bind", type=private_bind, default="127.0.0.1",
                        help="explicit loopback or private LAN IPv4; never wildcard")
    parser.add_argument("--port", type=int, default=18088)
    parser.add_argument("--delay-ms", type=int, default=50)
    parser.add_argument("--max-inflight", type=int, default=4)
    parser.add_argument("--lifetime", type=int, default=600, help="auto-stop seconds, max 1800")
    parser.add_argument("--log", required=True, type=Path,
                        help="new ledger path, must not already exist")
    args = parser.parse_args()
    if not (1024 <= args.port <= 65535 and 0 <= args.delay_ms <= 1000 and
            1 <= args.max_inflight <= 32 and 10 <= args.lifetime <= 1800):
        parser.error("limits: port 1024..65535; delay 0..1000ms; inflight 1..32; lifetime 10..1800s")
    if not args.log.parent.is_dir():
        parser.error("the ledger directory must exist")
    if args.log.exists():
        parser.error("refusing to overwrite an existing ledger")
    try:
        server = ReferenceServer((args.bind, args.port), args.log, args.delay_ms, args.max_inflight)
    except (OSError, FileExistsError) as exc:
        parser.error(str(exc))
    timer = threading.Timer(args.lifetime, server.shutdown)
    timer.daemon = True
    timer.start()
    try:
        print(f"Synthetic target: http://{args.bind}:{server.server_port}/work; lifetime {args.lifetime}s", flush=True)
        server.serve_forever(poll_interval=0.1)
    except KeyboardInterrupt:
        pass
    finally:
        timer.cancel()
        server.server_close()
        print("Synthetic target stopped; independent request ledger closed.", flush=True)


if __name__ == "__main__":
    main()
