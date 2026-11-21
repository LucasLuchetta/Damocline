"""Run the bounded CPU-quota falsification through the campaign and goal CLIs."""

import concurrent.futures
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import uuid

IMAGE = "alpine:3.22"
RATE = 25
BASE = "http://127.0.0.1:18080"
ROOT = Path(__file__).resolve().parent.parent


def command(*args, timeout=30, codes=(0,), **kwargs):
    result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True, timeout=timeout, **kwargs)
    if result.returncode not in codes:
        raise RuntimeError(f"{args[0]} exited {result.returncode}:\n{result.stdout}{result.stderr}")
    return result


def require(condition, description):
    if not condition:
        raise RuntimeError(description)


def read_metrics(target):
    body = command("docker", "exec", target, "wget", "-qO-", BASE + "/metrics").stdout
    return {name: int(value) for name, value in re.findall(
        r'^lab_requests_total\{outcome="(completed|rejected)"\} (\d+)$', body, re.MULTILINE
    )}


def run_case(root, binaries, constrained):
    label = "limited" if constrained else "control"
    folder = root / label
    folder.mkdir()
    name = "damocline-quota-" + uuid.uuid4().hex
    generator = name + "-generator"
    output_mount = ("--mount", f"type=bind,source={folder},target=/output")
    binary_mount = ("--mount", f"type=bind,source={binaries},target=/binlab,readonly")
    scenario = dict(
        name="CPU quota reference", base_url=BASE, mode="arrival", users=15, arrival_rate=RATE,
        duration="30s", window_duration="10s", request_timeout="2s", max_rps=50,
        p95_limit_ms=500, max_error_rate=0, min_samples_per_step=1,
        baseline_path="/healthz",
        workload_revision="synthetic-v1", application_revision="fixture-v1", environment="lab",
        steps=[dict(name="work", method="GET", path="/work", expected_status=[200])],
    )
    (folder / "scenario.json").write_text(json.dumps(scenario), encoding="utf-8")
    try:
        command("docker", "run", "-d", "--rm", "--name", name, "--network", "none", *binary_mount,
                IMAGE, "/binlab/target", "--listen", "127.0.0.1:18080", "--delay", "20ms",
                "--parallel", "15", "--queue", "15")
        target_quota = command("docker", "exec", name, "cat", "/sys/fs/cgroup/cpu.max").stdout
        require(target_quota.split()[0] == "max", "the target unexpectedly had a CPU quota")
        (folder / "target-cpu.max").write_text(target_quota, encoding="utf-8")
        for _ in range(10):
            try:
                require(read_metrics(name) == {"completed": 0, "rejected": 0}, "target ledger is not fresh")
                break
            except RuntimeError:
                time.sleep(0.2)
        else:
            raise RuntimeError("target did not start with zero counters")

        # Both arms pass the same calibration preflight before intervention.
        # The bounded container owns the burner and the generator. The target
        # shares loopback, while retaining its own independent CPU cgroup.
        script = """
            /binlab/damocline calibrate --config /output/scenario.json \\
                --level-duration 10s --response-bytes 24 --server-delay 20ms \\
                --out /output/calibration.json || exit 2
            (while :; do :; done) &
            burner=$!
            /binlab/damocline arrival-campaign --config /output/scenario.json \\
                --warmup 0s --observation 30s --recovery 10s --repeats 3 \\
                --peak-journeys-per-second 25 --stability-max-p95-increase-ms 100 \\
                --extended-envelope --extended-load-authorized --target-class lab \\
                --calibration /output/calibration.json --require-calibration \\
                --authorized --out /output/campaign.json
            status=$?
            kill "$burner"
            wait "$burner" 2>/dev/null
            cat /sys/fs/cgroup/cpu.max > /output/cpu.max
            cat /sys/fs/cgroup/cpu.stat > /output/cpu.stat
            exit "$status"
        """
        quota_applied = False
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            campaign = pool.submit(
                command, "docker", "run", "--rm", "--name", generator, "--network", f"container:{name}",
                "-e", "GOMAXPROCS=4", *binary_mount, *output_mount, IMAGE, "sh", "-c", script,
                timeout=185, codes=(0, 1, 2),
            )
            if constrained:
                deadline = time.monotonic() + 60
                while not campaign.done() and time.monotonic() < deadline:
                    if read_metrics(name)["completed"] >= 10:
                        command("docker", "update", "--cpu-period", "100000", "--cpu-quota", "5000", generator)
                        quota_applied = True
                        break
                    time.sleep(0.1)
            result = campaign.result()
        (folder / "campaign.log").write_text(result.stdout + result.stderr, encoding="utf-8")
        require(result.returncode != 2, result.stdout + result.stderr)
        require(not constrained or quota_applied, "quota was not applied during observation")
        require("campaign assessment error" not in result.stderr, result.stderr)
        report = json.loads((folder / "campaign.json").read_text(encoding="utf-8"))
        ledger = read_metrics(name)
        (folder / "target-ledger.json").write_text(json.dumps(ledger), encoding="utf-8")
        audit = command("docker", "run", "--rm", "--network", "none", *binary_mount, *output_mount,
                        IMAGE, "/binlab/damocline", "goal", "--file", "/output/campaign.json",
                        "--peak-journeys-per-second", str(RATE), "--out", "/output/goal.json")
        (folder / "goal.log").write_text(audit.stdout + audit.stderr, encoding="utf-8")
        assessment = json.loads((folder / "goal.json").read_text(encoding="utf-8"))
        state = assessment["State"]
        phases = [phase for trial in report["trials"] for phase in trial["phases"]]
        scheduled = sum(phase["run"]["arrivals_scheduled"] for phase in phases)
        dropped = sum(phase["run"]["arrivals_dropped"] for phase in phases)
        received = sum(phase["run"]["metrics"]["total"] for phase in phases)
        succeeded = sum(phase["run"]["metrics"]["success"] for phase in phases)
        require(ledger["rejected"] == 0 and ledger["completed"] == received == succeeded,
                f"{label}: target={ledger}, client requests={received}, client successes={succeeded}")
        stats = dict(line.split() for line in (folder / "cpu.stat").read_text().splitlines())
        throttled = int(stats["nr_throttled"])
        limit = (folder / "cpu.max").read_text().split()
        if constrained:
            require(limit == ["5000", "100000"], f"kernel reported an unexpected quota: {limit}")
            require(throttled > 0 and 0 < received < scheduled and dropped == scheduled - received,
                    "limited case did not demonstrate partial underdelivery and CPU throttling")
            require(any(p["evidence_state"] == "INVALID_DELIVERY" for p in phases), "delivery loss was hidden")
            require(report["outcome"] == "INCONCLUSIVE" and state == "INSUFFICIENT_EVIDENCE",
                    "underdelivery became a passing campaign or a decisive offline conclusion")
            require(assessment["MatchedStartsPerSecond"] is None and assessment["MatchedTrials"] == 0,
                    "offline goal credited sustained capacity despite underdelivery")
            require("BUSINESS GOAL: INSUFFICIENT_EVIDENCE" in result.stdout,
                    "embedded campaign assessment disagrees with offline goal")
        else:
            require(limit[0] == "max", f"control unexpectedly had a CPU quota: {limit}")
            require(throttled == 0 and received == scheduled == 3000 and dropped == 0 and report["outcome"] == "PASS",
                    "healthy control did not deliver and pass all six configured phases")
            require(state == "SUSTAINED_TARGET_PASSED_UNDER_TESTED_CONDITIONS" and
                    assessment["MatchedStartsPerSecond"] == RATE and assessment["MatchedTrials"] == 3,
                    f"healthy control did not receive sustained credit: {assessment['Findings']}")
        print(f"{label}: scheduled={scheduled} completed={received} rejected={ledger['rejected']} "
              f"dropped={dropped} throttle_events={throttled} campaign={report['outcome']} goal={state}", flush=True)
    finally:
        command("docker", "rm", "-f", generator, name, codes=(0, 1))


def main():
    with tempfile.TemporaryDirectory(prefix="damocline-quota-") as temp:
        binaries = Path(temp)
        records = ROOT / ".damocline" / ("arrival-cpu-quota-" + uuid.uuid4().hex)
        records.mkdir(parents=True)
        print(f"Evidence: {records.relative_to(ROOT)}", flush=True)
        env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0")
        for name, cmd in (("damocline", "damocline"), ("target", "damocline-labtarget")):
            command("go", "build", "-o", str(binaries / name), f"./cmd/{cmd}", env=env, timeout=90)
            (binaries / name).chmod(0o755)
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            jobs = [pool.submit(run_case, records, binaries, constrained) for constrained in (False, True)]
            for job in concurrent.futures.as_completed(jobs):
                job.result()


if __name__ == "__main__":
    main()
