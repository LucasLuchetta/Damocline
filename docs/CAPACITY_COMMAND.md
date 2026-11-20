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
of the reference report, and proposes a lower `--max-users`. After
exploration it warns again when the campaign level ran at 90% or more of
`max_rps`. Calibration sizing that exceeds the self-calibration level or
wall ceilings is refused with a proposed adjustment. A numeric observation
forecast requires a matching exploration artifact.

Standard ceilings apply by default. `--extended-envelope` (with
`--extended-load-authorized` and `--target-class lab|staging`) allows
`--max-users` up to 500. The protocol forwards the opt-in to calibration,
exploration and campaign (ADR 0170). It self-calibrates the ladder and
bisection fill-in levels, because extended refinement probes only calibrated
levels. If no calibrated level remains inside the failing bracket, the
campaign confirms the highest passing tested level, and the bracket is
reported as not refined.

The explore budget counts every stage, including reserved refinement probes,
as the stage duration plus the per-stage overhead. The overhead is the
network baseline cap, the bounded drain and setup time. Dry-run prints this
sizing. A plan above the 15m explore cap is refused before traffic.

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
damocline import-har --file capture.har --origin https://app.example.com --out proposal.json
```

Review the generated read-only proposal before turning it into a scenario.
Fill in omitted paths and required assertions by hand. Repeat
`--keep-path` to retain explicitly reviewed public routes. Cookie and token
headers are discarded. A session flag indicates observed credential
signals or an authorization response. Keep HARs and proposals local.

With a scenario-wide per-VU cookie prefix, pass `--session-hook` with the
executable and repeated `--session-hook-arg` literal arguments. It reads
`DAMOCLINE_SESSION_COUNT` and prints exactly that many distinct Cookie header
strings in a JSON array. No hook is executed during dry-run. Its stdout is
bounded, and its diagnostics and cookies are excluded from artifacts.
