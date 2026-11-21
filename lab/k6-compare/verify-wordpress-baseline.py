"""Run one bounded, read-only WordPress comparison with independent Apache logs."""

from datetime import datetime, timezone
import json
import math
import os
from pathlib import Path
import re
import secrets
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
BASE = "http://127.0.0.1"
ALPINE = "alpine:3.22"
K6 = "grafana/k6@sha256:e66db15b860113878fa74670e31f5e274830b7b6e42c8bff28b2f2d86a257603"
DURATION = 30
MAX_RPS = 10
P95_LIMIT = 500
MARKER = "rq3-synthetic-published-marker"


def call(*args, env=None, timeout=30, allowed=(0,)):
    result = subprocess.run(args, cwd=ROOT, env=env, capture_output=True, text=True, timeout=timeout)
    if result.returncode not in allowed:
        # Command arguments can contain disposable installation credentials.
        raise RuntimeError(f"{args[0]} exited {result.returncode}: {result.stderr.strip()}")
    return result


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def save(path, data):
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def access_slice(before, after, path):
    require(after.startswith(before), "Apache access log rotated during the run")
    rows = []
    for line in after[len(before):].splitlines():
        match = re.fullmatch(r"DAMOCLINE_HTTP (\d+) (\d+) (\d+) (\S+) (\S+)", line)
        require(match is not None, "unrecognized Apache access record")
        micros, elapsed, status, method, route = match.groups()
        if route == path:
            stamp = datetime.fromtimestamp(int(micros) / 1_000_000, timezone.utc).isoformat()
            rows.append(dict(time=stamp, duration_ms=int(elapsed) / 1000, status=int(status), method=method, path=route))
    require(rows, "Apache recorded no benchmark requests")
    require(all(r["status"] == 200 and r["method"] == "GET" for r in rows), "unexpected server result")
    return rows


def server_p95(rows):
    durations = sorted(row["duration_ms"] for row in rows)
    return durations[math.ceil(.95 * len(durations)) - 1]


def experiment(folder, binary_dir):
    env = dict(os.environ, WORDPRESS_DB_PASSWORD=secrets.token_hex(24),
               WORDPRESS_DB_ROOT_PASSWORD=secrets.token_hex(24), DAMOCLINE_SENSOR_TOKEN=secrets.token_hex(24),
               DAMOCLINE_WORDPRESS_SENSOR_TLS_DIR=str(binary_dir), DAMOCLINE_WORDPRESS_PORT="0")
    project = "damocline-rq3-" + secrets.token_hex(5)
    compose = ("docker", "compose", "-p", project, "-f", "lab/compose.wordpress.yaml")

    def dc(*args, timeout=30):
        return call(*compose, *args, env=env, timeout=timeout).stdout

    def wp(*args):
        return dc("--profile", "tools", "run", "--rm", "-T", "wordpress-cli", "wp", *args, timeout=60).strip()

    def mount(source, target, readonly=False):
        return ("--mount", f"type=bind,source={source},target={target}" + (",readonly" if readonly else ""))

    output_mount = mount(folder, "/out")
    binary_mount = mount(binary_dir, "/binlab", True)

    def snapshot():
        return dc("exec", "-T", "wordpress", "cat", "/tmp/damocline-http.log")

    try:
        dc("up", "-d", "--wait", "--wait-timeout", "90", "database", "wordpress", timeout=120)
        wp("core", "install", "--url=" + BASE, "--title=Synthetic RQ-3 fixture", "--admin_user=local_fixture_admin",
           "--admin_password=" + secrets.token_hex(24), "--admin_email=fixture@example.invalid", "--skip-email")
        wp("rewrite", "structure", "/%postname%/")
        post_id = int(wp("post", "create", "--post_title=Published synthetic post", "--post_content=" + MARKER,
                         "--post_status=publish", "--porcelain"))
        path = f"/wp-json/wp/v2/posts/{post_id}"
        post = json.loads(wp("post", "get", str(post_id), "--format=json"))
        require(post["post_status"] == "publish" and post["post_content"] == MARKER, "unexpected persisted post")
        scenario = dict(
            name="RQ-3 read published WordPress post", workload_revision="wordpress-published-post-v1",
            application_revision="wordpress-7.1.3-php8.3-apache", environment="isolated-docker-compose-linux",
            base_url=BASE, mode="closed", users=1, duration=f"{DURATION}s", ramp_up="0s",
            request_timeout="2s", max_rps=MAX_RPS, p95_limit_ms=P95_LIMIT, max_error_rate=0,
            min_samples_per_step=100, baseline_path="/wp-login.php",
            steps=[dict(name="read_published_post", method="GET", path=path, expected_status=[200], must_contain=MARKER)],
        )
        save(folder / "scenario.json", scenario)
        generated = call("go", "run", "./lab/k6-compare/scenario-to-k6", str(folder / "scenario.json"), timeout=90)
        script = generated.stdout + "\nexport function handleSummary(data) { return {'/out/k6.json': JSON.stringify(data)}; }\n"
        (folder / "compare.js").write_text(script, encoding="utf-8")
        target = dc("ps", "-q", "wordpress").strip()
        require(re.fullmatch(r"[a-f0-9]{12,64}", target) is not None, "unable to identify the lab WordPress container")
        network = "container:" + target

        def warm():
            for _ in range(5):
                response = call("docker", "run", "--rm", "--network", network, ALPINE,
                                "wget", "-T", "3", "-qO-", BASE + path).stdout
                require(MARKER in response and json.loads(response)["id"] == post_id, "REST response disagrees with stored post")

        def measure(label, args):
            warm()
            before = snapshot()
            result = call("docker", "run", "--rm", "--network", network, *output_mount,
                          *args, timeout=80, allowed=(0, 1, 99))
            (folder / (label + ".log")).write_text(result.stdout + result.stderr, encoding="utf-8")
            rows = access_slice(before, snapshot(), path)
            with (folder / (label + "-apache.ndjson")).open("w", encoding="utf-8") as log:
                for row in rows:
                    log.write(json.dumps(row) + "\n")
            return rows

        dam_rows = measure("damocline", (*binary_mount, ALPINE, "/binlab/damocline", "run", "--config",
                                      "/out/scenario.json", "--authorized", "--out", "/out/damocline.json"))
        k6_rows = measure("k6", ("-e", "BASE_URL=" + BASE, K6, "run", "--quiet", "--rps", str(MAX_RPS), "/out/compare.js"))
        require(json.loads(wp("post", "get", str(post_id), "--format=json")) == post, "read workload changed the stored post")
        save(folder / "post-oracle.json", dict(id=post_id, status="publish", marker=MARKER, unchanged=True))

        for arguments in (
            ("crosscheck", "--run", "/out/damocline.json", "--access-log", "/out/damocline-apache.ndjson",
             "--format", "json", "--count-tolerance", "0", "--count-abs-tolerance", "0", "--out", "/out/crosscheck.json"),
            ("compare-external", "--damocline", "/out/damocline.json", "--k6-summary", "/out/k6.json",
             "--out", "/out/comparison.json"),
        ):
            call("docker", "run", "--rm", "--network", "none", *output_mount, *binary_mount, ALPINE,
                 "/binlab/damocline", *arguments, allowed=(0, 1))

        dam = json.loads((folder / "damocline.json").read_text(encoding="utf-8"))
        k6 = json.loads((folder / "k6.json").read_text(encoding="utf-8"))["metrics"]
        crosscheck = json.loads((folder / "crosscheck.json").read_text(encoding="utf-8"))
        comparison = json.loads((folder / "comparison.json").read_text(encoding="utf-8"))
        stats = dict(
            damocline_requests=dam["metrics"]["total"], damocline_apache_requests=len(dam_rows),
            k6_requests=k6["http_reqs"]["values"]["count"], k6_apache_requests=len(k6_rows),
            damocline_p95_ms=dam["metrics"]["p95_ms"], k6_p95_ms=k6["http_req_duration"]["values"]["p(95)"],
            damocline_apache_p95_ms=server_p95(dam_rows), k6_apache_p95_ms=server_p95(k6_rows),
            k6_body_status_check_passes=k6["checks"]["values"]["passes"],
            k6_body_status_check_failures=k6["checks"]["values"]["fails"],
            damocline_outcome=dam["outcome"], crosscheck=crosscheck["overall"], comparator=comparison["overall"],
        )
        save(folder / "baseline.json", stats)
        print(json.dumps(stats, indent=2), flush=True)
        require(stats["damocline_requests"] == len(dam_rows) and stats["k6_requests"] == len(k6_rows),
                "client and Apache request ledgers disagree")
        require(min(len(dam_rows), len(k6_rows)) >= 250 and abs(len(dam_rows) - len(k6_rows)) <= 30,
                "delivered counts are outside the declared baseline tolerance")
        require(dam["outcome"] == "PASS" and dam["metrics"]["success"] == len(dam_rows) and
                stats["k6_body_status_check_passes"] == len(k6_rows) and stats["k6_body_status_check_failures"] == 0 and
                comparison["k6_would_pass"],
                "the two clients did not both meet the declared success criteria")
        require(comparison["overall"] not in ("RED_FLAG", "NOT_COMPARABLE"), "external comparison flagged a contradiction")
        for client in ("damocline", "k6"):
            client_p95 = stats[client + "_p95_ms"]
            apache_p95 = stats[client + "_apache_p95_ms"]
            require(0 < client_p95 <= P95_LIMIT and 0 < apache_p95 <= P95_LIMIT and
                    abs(client_p95 - apache_p95) <= max(10, .25 * max(client_p95, apache_p95)),
                    f"{client} p95 and Apache p95 diverge beyond the declared tolerance")
    finally:
        dc("down", "--volumes", "--timeout", "5", timeout=60)


def main():
    folder = ROOT / ".damocline" / ("rq3-wordpress-" + secrets.token_hex(6))
    folder.mkdir(parents=True)
    print(f"Evidence: {folder.relative_to(ROOT)}", flush=True)
    with tempfile.TemporaryDirectory(prefix="damocline-rq3-") as temp:
        directory = Path(temp)
        binary = directory / "damocline"
        call("go", "build", "-o", str(binary), "./cmd/damocline", timeout=120,
             env=dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0"))
        binary.chmod(0o755)
        experiment(folder, directory)


if __name__ == "__main__":
    main()
