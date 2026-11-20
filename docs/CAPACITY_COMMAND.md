# Closed-loop capacity protocol

```powershell
damocline capacity --config scenario.json --dry-run
damocline capacity --config scenario.json --authorized --output-dir .damocline/capacity-run
damocline capacity --config scenario.json --dry-run --explore-file .damocline/capacity-run/explore.json
```

Use a local private scenario and artifact directory. The command uses a
read-only closed-loop workload, an explicit authorized network baseline,
optional existing `--calibration` evidence, exploration, a repeated campaign
at the highest passing explored level, and offline audit. Network placement
guidance reflects the measured baseline. Automatic scenario calibration is
pending. A forecast requires a matching exploration artifact. Without that
artifact, dry-run shows the formula and wall-time limits and sends no traffic.

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
