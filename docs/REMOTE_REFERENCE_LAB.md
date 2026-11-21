# RQ-3 — Independent-host LAN reference lab

This controlled protocol tests a native Damocline read-only run against a
**separate physical Ubuntu target**, with a Python standard-library HTTP fixture
that writes its own independent access ledger (NDJSON). This validates a network
path and server-side accounting; it is **not** a certification of application
capacity or a representative production application.

The fixture serves GET `/work`, returns a synthetic marker, sleeps for a bounded
service delay, and responds HTTP 503 when its configurable concurrency gate is
full. It excludes GET `/healthz` from the ledger. It only binds to explicit
loopback/RFC1918 IPv4, never a wildcard, and auto-stops after at most 30 minutes.
**No TLS or authentication:** use on a trusted isolated LAN; never forward its
port through your router, place it on the public Internet, or run it against
FluIA or any real customer application.

## 1. Ubuntu home server: TARGET (no Go or Docker needed)

Requirements: Python 3.9+, private LAN, checkout of Damocline. Find the
server's private IPv4 address:

```sh
hostname -I
python3 --version
python3 -m unittest discover -s lab -p test_remote_reference_target.py -v
```

From the repository directory on the Ubuntu server, replace
`192.168.1.42` with its own actual LAN IP:

```sh
mkdir -p .damocline/remote-ref
python3 lab/remote_reference_target.py \
  --bind 192.168.1.42 --port 18088 \
  --delay-ms 50 --max-inflight 4 --lifetime 600 \
  --log .damocline/remote-ref/server-01.ndjson
```

The target runs in the foreground until Ctrl+C or its 600-second automatic
shutdown. The log is newly created and cannot overwrite an earlier artifact.
Use a different log name for every fresh experiment. No other traffic should
hit `/work` while measuring, otherwise independent totals become ambiguous.

## 2. Windows PC: GENERATOR (physically separate)

Requirements: Git, Go 1.24+, a checkout of Damocline, and LAN connectivity
to the target. Open PowerShell in the repository directory and replace IP:

```powershell
$ip = '192.168.1.42'
Test-NetConnection $ip -Port 18088
(Invoke-WebRequest "http://$($ip):18088/healthz").Content

New-Item -ItemType Directory -Force .damocline/remote-ref | Out-Null
$s = Get-Content examples/remote-reference-readonly.json -Raw | ConvertFrom-Json
$s.base_url = "http://$($ip):18088"
$json = $s | ConvertTo-Json -Depth 12
$path = Join-Path (Get-Location) '.damocline/remote-ref/scenario.json'
[System.IO.File]::WriteAllText($path, $json, [System.Text.UTF8Encoding]::new($false))

go run ./cmd/damocline run --config .damocline/remote-ref/scenario.json --allow-insecure-http --dry-run
go run ./cmd/damocline run --config .damocline/remote-ref/scenario.json --allow-insecure-http --authorized --out .damocline/remote-ref/damocline-01.json
```

The smoke workload: **2 concurrent VUs, 30 seconds, one read-only GET,
100 request starts/s hard ceiling** (not a demand target), HTTP 200 and body
marker required. The scenario uses `baseline_path: /healthz` for the Damocline
pre-load network-path probe (20 requests); this route is intentionally excluded
from the `/work` ledger so reference counts cannot be polluted by calibration.
The 4-slot target is intentionally bounded. HTTP is allowed
only because both machines are on a trusted local network.

Do not run other generators concurrently, increase the envelope, or
retroactively weaken criteria to manufacture a passing result. Prefer a wired
LAN to reduce Wi-Fi contention. Record whether the generator and target were
on Wi-Fi or Ethernet.

## 3. Independent log reconciliation

After the test, copy the server's untouched
`.damocline/remote-ref/server-01.ndjson` into the generator's
`.damocline/remote-ref/server-01.ndjson` (e.g. through LAN `scp`). On the
Windows generator, from the repository directory:

```powershell
go run ./cmd/damocline crosscheck --run .damocline/remote-ref/damocline-01.json --access-log .damocline/remote-ref/server-01.ndjson --format json --count-tolerance 0 --count-abs-tolerance 0 --out .damocline/remote-ref/crosscheck-01.json
```

An exact match requires independent logged HTTP request/status counts to
reconcile, and the measured server p95 not to exceed the client p95 beyond
the declared tolerance. A `MISMATCH` or `NOT_ASSESSABLE` is **not approval**.
Clock skew can obscure matching; check Ubuntu `timedatectl status` and the
Windows date/time settings. Never silently trim logs to make a count match.

## 4. Next controlled falsification

For a saturation case, start a **new** fixture with a new ledger filename,
change the scenario `users` to 8, and run the same bounded duration with
a different client report filename. Some 503s may be observed. An HTTP 503
contradicts the expected status/body marker, so Damocline must not approve
the affected level. Reconcile it against the fresh independent ledger.

A separate k6 comparison can follow with the same workload using
`lab/k6-compare/scenario-to-k6` and `damocline compare-external`, as described
in `lab/k6-compare/README.md`. Use a **fresh target fixture and log** for
k6, with no concurrent Damocline run; verify the same user count, criteria,
target state, and request ceiling before comparing. This fixture is one
controlled scenario, not the full RQ-3 representative-application matrix.

Keep the scenario, Damocline JSON, crosscheck JSON and raw server ledger
private. Record the versions of Go/Python, both machines' CPU allocations
and network topology. Public evidence may publish redacted aggregate counts
and hashes, not private addresses, host names or client identifiers.
