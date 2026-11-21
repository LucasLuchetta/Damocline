# Closed-loop capacity protocol

```powershell
damocline capacity --config scenario.json --dry-run
damocline capacity --config scenario.json --authorized --output-dir .damocline/capacity-run --reference-report .damocline/reference-run.json
damocline capacity --config scenario.json --dry-run --explore-file .damocline/capacity-run/explore.json
```

Use a local private scenario and artifact directory. The command uses a
read-only closed-loop workload, an explicit authorized network baseline,
workload self-calibration (`damocline calibrate`, ADR 0154) of every planned
exploration level and later of the chosen campaign level, exploration with
`--calibration --require-calibration`, a repeated campaign at the highest
passing explored level with the same gate, and offline audit. Network
placement guidance reflects the measured baseline.

Self-calibration replays the scenario shape against a loopback mirror only;
it never contacts the target. Size the mirror with `--reference-report` (a
recorded run of the target supplies per-step sizes and median latency) or
with `--response-bytes` and `--server-delay`. An existing passing
`--calibration` self-calibration artifact may replace the automatic step.
A failed calibration stops the protocol before target workload traffic.

Dry-run prints every stage, its wall cap, the sizing formula and the total
planned wall time. It warns when the scenario `max_rps` would cap
throughput below the requested level, estimated from the per-VU request rate
of the reference report, and proposes a lower `--max-users`. It also warns when estimated delivery
is at least 90% of the ceiling, where exploration may be inconclusive. If the
reference itself reached 90% of its recorded ceiling, dry-run explains why
it cannot forecast unconstrained delivery from that reference. Explore stage
sizing accounts for the planned request ceiling. After
exploration it warns again when the campaign level ran at 90% or more of
`max_rps`. Calibration sizing that exceeds the self-calibration level or
wall ceilings is refused with a proposed adjustment. A numeric observation
forecast requires a matching exploration artifact.

Standard ceilings apply. `--extended-envelope` (with
`--extended-load-authorized` and `--target-class lab|staging`) passes the
ADR 0155 extended gate to calibration only; exploration and campaign stages
still run within the standard 50 VU ceiling in this release.

The explore budget is sized for the worst case. That case is the ladder up
to a failing level plus that bracket's refinement probes, each run
`repeats` times. Each stage counts as its duration plus its overhead: the
network baseline wall cap, the engine's bounded drain and 2s of setup
(ADR 0170). Dry-run prints this sizing. A plan above the 15m explore cap is
refused before traffic, with a proposal to lower `--max-users` or shorten
the stages.

The protocol uses the weakest measured delivery rate across all relevant
series and repeats. For a 1% error budget, the zero-failure conditional
minimum is 299 samples for each series in each window. The plan also
requires enough samples for a conditional p95 rank interval. More time
cannot overcome a hard window ceiling. Simplify the workload or validate
more VUs within the supported ceiling in that case. A larger total budget
within existing limits can help when the budget is the binding constraint.
Long runs require explicit staging opt-in. The estimate reserves maximum
preflight and drain time and does not guarantee future sample delivery.

Re-run with `--resume` and the same scenario and exploration controls to
continue after interruption. Increase `--budget` explicitly if planning
stopped before the campaign. A saved campaign retains its recorded budget.
The command reuses completed stages and preserves interrupted evidence
before repeating it. After a hard kill, remove the local `active` marker
only after verifying the old process ended.

Use `--actions-per-user-minute` or `--access-log` to add a usage hypothesis.
The local log input is JSON lines with a pseudonymous `user` and an RFC3339
`timestamp` per successful action. The estimate divides mean observed
successful HTTP requests per minute by the supplied intensity. Using a log
divides its actions by distinct observed identities and capture duration.
Review the one-request-per-action and workload-mix assumptions.

```powershell
damocline import-har --har capture.har --base-url https://app.example.com --out scenario.json
```

The command converts a local browser HAR into a scenario draft without
sending traffic (ADR 0178). Only same-origin requests are kept; static assets
are dropped unless `--keep-static` is given, and POST, PUT, PATCH and DELETE
are dropped unless `--include-writes` is given (running them still needs
`--allow-writes`). Cookies, authorization and token headers become `${ENV}`
placeholders, request bodies become `body_env` placeholders, and sensitive
query parameters are removed. The command lists every dropped request and
every placeholder to set. Review paths, statuses, think time and pass
criteria before running the draft. Keep HARs and drafts local.

With a scenario-wide per-VU cookie prefix, pass `--session-hook` with the
executable and repeated `--session-hook-arg` literal arguments. It reads
`DAMOCLINE_SESSION_COUNT` and prints exactly that many distinct Cookie header
strings in a JSON array. No hook is executed during dry-run. Its stdout is
bounded, and its diagnostics and cookies are excluded from artifacts.

## Executive HTML report

After the offline audit, the protocol writes `capacity.html` next to
`capacity.md` in the output directory (ADR 0173). It is generated from the
saved campaign and exploration only, with no extra traffic, and plots the
exploration probes and campaign phases against the objectives. A declared
`--actions-per-user-minute` or `--access-log` intensity appears only as a
labeled assumption. The file is created with mode 0600 and never overwritten;
directories completed by an earlier release keep only the Markdown report.
