"""Stdlib regression checks for the independent-LAN reference target."""

import argparse
import json
from pathlib import Path
import tempfile
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import urlopen

from remote_reference_target import MARKER, ReferenceServer, private_bind


class RemoteReferenceTests(unittest.TestCase):
    def test_private_only_binding(self):
        for address in ("127.0.0.1", "192.168.1.8", "10.9.0.6", "172.16.12.4"):
            self.assertEqual(private_bind(address), address)
        for address in ("0.0.0.0", "1.1.1.1", "169.254.1.1", "::1", "localhost"):
            with self.assertRaises(argparse.ArgumentTypeError):
                private_bind(address)

    def test_ledger_reconciles_healthy_and_saturated_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            ledger = Path(directory) / "ledger.ndjson"
            server = ReferenceServer(("127.0.0.1", 0), ledger, delay_ms=200, max_inflight=1)
            background = threading.Thread(target=server.serve_forever, daemon=True)
            background.start()
            base = f"http://127.0.0.1:{server.server_port}"
            received = []

            def first():
                with urlopen(base + "/work", timeout=5) as response:
                    received.append((response.status, response.read()))

            try:
                with urlopen(base + "/healthz", timeout=2) as response:
                    self.assertEqual(response.read(), b"ok")
                worker = threading.Thread(target=first)
                worker.start()
                self.assertTrue(server.active.wait(2), "worker did not begin")
                with self.assertRaises(HTTPError) as caught:
                    urlopen(base + "/work", timeout=2)
                self.assertEqual(caught.exception.code, 503)
                worker.join(3)
                self.assertEqual(received, [(200, MARKER)])
            finally:
                server.shutdown()
                background.join(2)
                server.server_close()

            rows = [json.loads(line) for line in ledger.read_text(encoding="utf-8").splitlines()]
            self.assertEqual(sorted(row["status"] for row in rows), [200, 503])
            self.assertTrue(all(row["method"] == "GET" and row["path"] == "/work" for row in rows))
            self.assertTrue(all(row["duration_ms"] >= 0 and row["time"].endswith("+00:00") for row in rows))
            self.assertGreater(ledger.stat().st_size, 0)

    def test_ledger_never_overwrites_existing_file(self):
        with tempfile.TemporaryDirectory() as directory:
            ledger = Path(directory) / "existing.ndjson"
            ledger.write_text("existing evidence\n")
            with self.assertRaises(FileExistsError):
                ReferenceServer(("127.0.0.1", 0), ledger, delay_ms=0, max_inflight=1)
            self.assertEqual(ledger.read_text(), "existing evidence\n")


if __name__ == "__main__":
    unittest.main()
