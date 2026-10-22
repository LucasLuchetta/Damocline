# Controlled Linux capacity lab

This lab runs three separate containers: a synthetic HTTP target with one work
slot, an authenticated Linux host sensor, and the Damocline generator. The
target's queue and work delay are deliberately bounded. Its `/metrics` endpoint
reports completed and rejected requests plus current activity. It also
publishes `process_start_time_seconds` so campaign reports can verify
exporter-reported process identity changes. All traffic is local to the
Compose stack; the target also publishes a loopback-only port for inspection.

Create a private local Compose environment file once. It is gitignored; do not
commit it or put its value in command arguments:

```powershell
Set-Content -NoNewline lab/.env "DAMOCLINE_SENSOR_TOKEN=$([guid]::NewGuid().ToString('N'))"
```

With Docker Compose available, run from the repository root:

```sh
docker compose --env-file lab/.env -f lab/compose.yaml up --build -d target sensor
docker compose --env-file lab/.env -f lab/compose.yaml --profile run run --rm generator
docker compose --env-file lab/.env -f lab/compose.yaml down
```

The generator writes `lab/artifacts/lab-campaign.json`, which is gitignored.
Inspect its experiment ID, phase outcomes, application metrics coverage, host
sensor coverage, and clock-offset bounds. Re-run with a higher user count or a
smaller queue in a private scenario to provoke the known queue restriction;
the expected outcome is an observed breach or an explicit evidence limitation,
never a certified universal capacity limit. Stop the sensor during a separate
run to verify that missing host readings make the campaign inconclusive.

`scenario-overload.json` provides a bounded 8-VU fixture for the queue
restriction. Keep its report separate from the baseline report when comparing
the two; they intentionally have different workload fingerprints.

To run the two negative controls while the target is running:

```sh
docker compose --env-file lab/.env -f lab/compose.yaml -f lab/compose.overload.yaml --profile run run --rm generator
docker compose --env-file lab/.env -f lab/compose.yaml stop sensor
docker compose --env-file lab/.env -f lab/compose.yaml -f lab/compose.sensor-outage.yaml --profile run run --rm --no-deps generator
docker compose --env-file lab/.env -f lab/compose.yaml start sensor
```

The negative-control generator commands may exit with status 1 because their
expected outcomes are failure or inconclusive. Inspect the saved JSON reports
for the observed evidence state and the exact reason.

To repeat the bounded queue-overload control and its offline audit on Windows
PowerShell:

```powershell
.\lab\verify-target-overload.ps1
```

`verify-spike-recovery.ps1` runs a bounded baseline observation, a 20-VU spike,
and a 2-VU recovery against the synthetic queue-limited target. It checks that
the campaign retains a spike SLO failure while recording recovery, complete
telemetry, and generator health. The campaign and offline audit are saved
under `lab/artifacts/`.

The independent `verify-target-overload.ps1` script covers sustained target
overload; the spike-recovery script runs only its declared observation, spike,
and recovery phases.

The script uses an isolated Compose project and loopback port, runs the
predeclared 20-VU workload for at most 10 seconds, and stops after the first
observed SLO breach even though two trials were planned. It retains the
predeclared 10-second recovery setting but does not run that phase after the
observation itself fails. It requires HTTP 503s,
a rising synthetic-target rejection counter, complete app/host telemetry, zero
dropped or pre-admission-cancelled work, and valid generator CPU headroom. The
expected campaign outcome is `FAIL`; its offline capacity audit remains
`INCONCLUSIVE` while the observation is non-stationary or generator quality is
limited. Timestamped campaign and audit files go to `lab/artifacts/` without
overwriting earlier evidence.

The overload verifier also writes a separate timestamped reconciliation artifact.
For each observed phase it compares the increases between first and last samples
of `lab_requests_total{outcome="completed"}` and
`lab_requests_total{outcome="rejected"}` with independently measured HTTP 200
and HTTP 503 response counts. Counter resets or an increase above the matching
HTTP count fail the check. A smaller increase is retained with its unsampled
difference because periodic snapshots do not cover every request boundary; the
lab does not claim exact request-by-request reconciliation.

To repeat the required-sensor outage as one bounded, self-cleaning check on
Windows PowerShell:

```powershell
.\lab\verify-sensor-outage.ps1
```

The script creates an isolated Compose project on a dynamically selected
loopback port, stops only its own sensor, runs the minimum two planned trials
with a 10-second observation bound, and recalculates the saved campaign offline.
The campaign may stop after the first trial when required telemetry is missing;
the expected result is HTTP `PASS`, host telemetry `INCOMPLETE`, and campaign
plus audit `INCONCLUSIVE`. Artifacts are timestamped under `lab/artifacts/` and
never overwrite previous reports.

The local protocol replay records its sealed protocol and private artifacts in
`lab/artifacts/`. The sealed capacity protocol retains separate passing
candidate campaigns and counts each distinct failing-level campaign only as an
observed first-breach replication; the controlled fixture stops each failure
campaign after its first valid breach. Its current assessment remains
`INSUFFICIENT_EVIDENCE` because ordinary generator evidence is `LIMITED`, and
it issues no operational capacity recommendation. The separate
`lab-protocol-spike.json` protocol exercised a
10-second observation, a bounded 4-VU spike for 10 seconds, and a 10-second
recovery in two independent candidate campaigns, each with two trials. Both
campaigns observed the configured SLO pass during the spike and recovery, but
their measurement quality remains LIMITED; this synthetic lab is not an
authorized real-application validation or a production capacity claim. Newer
sensor samples retain selected-process RSS and explicit unknown cgroup limits
when Docker Desktop hides the target hierarchy. The paired Markdown report,
structured campaign audit, and replay bundle remain
gitignored alongside their source campaign JSON files.

Recalculate the separate spike assessment without generating traffic:

```sh
go run ./cmd/damocline protocol assess \
  --file lab/artifacts/lab-protocol-spike.json \
  --exploration-campaign lab/artifacts/protocol-spike-exploration-1.json \
  --confirmation-campaign lab/artifacts/protocol-spike-confirmation-1.json \
  --confirmation-campaign lab/artifacts/protocol-spike-confirmation-2.json \
  --out lab/artifacts/protocol-spike-assessment.json \
  --markdown lab/artifacts/capacity-spike-assessment.md \
  --bundle lab/artifacts/spike-reproduction-bundle.json
```

The sensor shares the target's network and PID namespaces so the generator can
reach its loopback-only endpoint. PID 1 selects the target process. Docker
Engine mounts differ: where the target cgroup is visible to the sensor it
records quota and memory counters; where it is hidden, those values remain
unknown. For the Windows Docker Desktop run, `docker inspect` and a read inside
the target container independently confirmed the 0.50 CPU/256 MiB constraints;
the phase sensor record remains host-wide because the VM cgroup path cannot be
bind-mounted from Windows. The latest controlled campaign measured selected
process RSS and CPU, host swap, and per-sample disk/network deltas; it still
cannot see the target cgroup. To reproduce the process restart check on Windows PowerShell, run:

```powershell
.\lab\verify-process-restart.ps1
```

The script uses a temporary random token only to satisfy Compose interpolation
(it does not start the sensor), plus a unique Compose project and loopback
port. It restarts only its bounded synthetic target, refuses to overwrite
existing evidence, writes a timestamped JSON file under `lab/artifacts/`, and
removes the temporary token file and project in a `finally` cleanup. The
captured `process_start_time_seconds` pair proves one
exporter-reported process restart; it does not provide a Docker Engine restart
count. When the fixed `process-restart-evidence.json` fixture is present,
`go test ./internal/appmetrics` also feeds those captured values to the restart
detector. A production-like representative application remains open work.

## Installed open-source application check

`compose.wordpress.yaml` adds a separate, disposable WordPress and MariaDB lab.
It installs the pinned WordPress 7.1.3 PHP 8.3 Apache image with MariaDB 11.8.9,
creates one synthetic post, and tests only the homepage and read-only posts API.
The scenario defaults to two closed-loop virtual users; its 100 requests/second
`max_rps` setting is a safety ceiling, not the target rate. It was raised after
the earlier 10 requests/second ceiling triggered `CLIENT_RATE_CEILING_NEAR`.
The bounded verifier accepts `-Users` from 1 to 50 and `-MaxRps` from 1 to 100
so exploratory and confirmation campaigns can hold the workflow fixed while
changing the declared closed-loop VU level. The configured request-rate ceiling
remains a guardrail and does not define the offered workload.
The verifier also accepts `-WordPressCpuLimit` values `0.75`, `1.0`, or `1.5`
(default `0.75`) for bounded diagnostic interventions. A nondefault value is
recorded as a distinct environment and application revision; never combine
those measurements with the default-quota protocol. This option is for
controlled fixture comparisons and does not certify a production scaling
recommendation.
The images are published as official images by the [WordPress project](https://hub.docker.com/_/wordpress)
and [MariaDB project](https://hub.docker.com/_/mariadb). The stack publishes
WordPress on loopback only, limits application, database, and generator CPU and
memory. A read-only `.htaccess` fixture enables the REST rewrite rule, and the
local-only WordPress canonical URL follows the loopback `Host` header so the
dynamic host port works for local route checks. For the campaign, the verifier
writes a private scenario targeting the Compose DNS name `wordpress`; the
generator runs in its own container/network namespace on the Compose bridge,
separate from the application and sensor namespaces. It reaches the
authenticated HTTPS sensor at `wordpress:9467`; the verifier creates a
short-lived DNS certificate for the private Compose service name, mounts its
public half into the generator as a trust root, makes the key readable by the
unprivileged sensor only during the run, and removes the key and certificate
after the stack stops. The application and metrics routes use
explicit `--allow-insecure-http` only for these isolated internal HTTP
endpoints. This is network separation on one Docker host, not a remote
generator or production network path. The script removes its database and
WordPress volumes after each run.

Create a private, gitignored `lab/.env.wordpress` with fresh local-only values
before running the test. PowerShell example:

```powershell
$token = [guid]::NewGuid().ToString('N')
@(
  "DAMOCLINE_SENSOR_TOKEN=$token"
  "WORDPRESS_DB_PASSWORD=$([guid]::NewGuid().ToString('N'))"
  "WORDPRESS_DB_ROOT_PASSWORD=$([guid]::NewGuid().ToString('N'))"
) | Set-Content -Encoding ascii lab/.env.wordpress
```

Then run:

```powershell
.\lab\verify-wordpress.ps1
```

To test whether a longer cache/application warm-up reduces early-window drift,
use a bounded value up to 120 seconds, for example:

```powershell
.\lab\verify-wordpress.ps1 -WarmupSeconds 60
```

For example, a bounded 4-VU exploration can use
`.\lab\verify-wordpress.ps1 -WarmupSeconds 0 -Users 4 -MaxRps 100`.

A diagnostic CPU-quota comparison can use
`.\lab\verify-wordpress.ps1 -WarmupSeconds 0 -Users 10 -MaxRps 100 -RecoverySeconds 30 -WordPressCpuLimit 1.5`.
This changes only the WordPress container CPU quota; it records a distinct
application revision/environment and remains a synthetic fixture experiment.

The script installs WordPress with an ephemeral admin credential, verifies both
read-only routes, and collects ten selected MariaDB status values through a
read-only PHP exposition endpoint alongside authenticated Linux host
telemetry. The endpoint exposes no labels, table contents, or SQL errors; it is
reachable only through the loopback-published fixture. Every scrape performs
one status query, so its connection is included in thread gauges and its query
is included in server counters. The script requires the bounded Linux generator
preflight to pass, then
plans up to two trials with a configurable 0–120-second warm-up (30 seconds by
default), 60-second observation, and
30-second recovery under a 7-minute campaign budget. The verifier accepts a
10-180-second recovery setting through `-RecoverySeconds`; use the same value
for every campaign included in a sealed protocol. The campaign stops early
when evidence is inconclusive. Each
phase includes authenticated Linux host telemetry, complete MariaDB status
scrapes with `damocline_mariadb_up == 1`, and generator headroom checks. These
dependency metrics alone do not establish a root cause. Apache timing below
provides server-side request handling duration, not WordPress/PHP
instrumentation.
The fixture also enables a bounded Apache access-log format containing only
start time, server duration, status, method, and path. WordPress rewrites `/`
to `/index.php` internally; the verifier maps that Apache path back to the
declared homepage step. It retains records for the two allowlisted read-only
routes and assigns them to observation windows; it does not retain query
strings, headers, cookies, or bodies. This server-side duration is not isolated
PHP execution time, does not establish a root cause, and has unmeasured logging
overhead. Timestamped calibration,
campaign, offline-audit, and Apache timing artifacts stay under
`lab/artifacts/`; the private database and WordPress volumes are removed during
cleanup. The synthetic workflow is not a user's application or a representative
production mix. Generator preflight does not establish endpoint, response-shape,
or workload equivalence, so a capacity recommendation is not expected.

During the campaign, the verifier also samples Docker Engine statistics for
container IDs carrying only its unique Compose project label. It requires at
least three samples per service/window; the requested one-second delay follows
each stats call, so actual cadence includes call duration.
Its gitignored resource artifact contains service role, CPU percentage as
reported by Docker, memory used/limit, sample time, and stats-call duration.
It also records point-in-time restart/OOM state and filtered lifecycle events
for the WordPress and database services, with explicit coverage status.
Where the container exposes cgroup v2, the artifact includes pre-campaign and
post-campaign CPU quota, usage/throttling, and memory event deltas; these span
the whole campaign rather than individual phases.
These whole-container readings supplement the host sensor where Docker Desktop
does not expose target cgroups. Host UTC is not calibrated against the engine's
monotonic phase timer, and boundary grouping is approximate. Docker polling
overhead is not measured; these metrics are diagnostic, not causal evidence.
