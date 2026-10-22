# Predeclared capacity confirmation protocol

The experimental `protocol` command records the decision rules before a
campaign begins, then compares separate saved campaigns against that sealed
record. It performs no network requests. A SHA-256 digest detects accidental
or later edits; it is not a signature, trusted timestamp, or proof of who
created the file. Keep the sealed protocol with the local reproduction bundle.

Start from [the draft example](../examples/capacity-protocol-draft.json),
replace its synthetic labels and fingerprint with the values for the authorized
scenario, and record preparation and stop rules before sending load:

```powershell
go run ./cmd/damocline protocol seal --file protocol-draft.json --out reports/protocol.json
```

The protocol fixes the target origin, workload and application revisions,
environment, fingerprint, candidate and optional first failing level, SLOs, stage schedule,
minimum observation, telemetry requirements, projected demand, growth, reserve,
preparation, and stop conditions. The current closed-loop campaign safety
limits constrain the observation to seven minutes and each other phase to
three minutes.

After one or more exploratory campaigns, run the candidate in the required
number of separate campaign executions; each passing candidate campaign must
contain at least two completed trials. For a predeclared failing level, keep
each execution bounded and stop after its first valid SLO breach. Distinct
failure campaigns that each retain such a breach count as independent observed
failure replications; they do not run additional trials after the breach. Then
replay the evidence:

```powershell
go run ./cmd/damocline protocol assess --file reports/protocol.json `
  --exploration-campaign reports/exploration.json `
  --confirmation-campaign reports/candidate-confirmation-1.json `
  --confirmation-campaign reports/candidate-confirmation-2.json `
  --failure-campaign reports/failure-confirmation-1.json `
  --failure-campaign reports/failure-confirmation-2.json `
  --out reports/protocol-assessment.json `
  --markdown reports/capacity-assessment.md `
  --bundle reports/reproduction-bundle.json
```

The assessor checks that the protocol predates every campaign, campaign
evidence is distinct, and declared identity, fingerprint, SLO, phase schedule,
duration, telemetry, and exact levels match. It reports the observed spread
between complete journey p95/error rates and computes a conditional planning level by
applying the declared reserve to the confirmed passing candidate. It never
interpolates levels. If a predeclared failure level was declared, separate failure
observations are mandatory. A safe early stop after the first valid breach is
retained as an observed failure replication and audited; the failure level is
never driven for further trials in that campaign. This is distinct from the
repeated passing-candidate requirement. The structured assessment JSON includes per-campaign
audit state, measurement quality, planned/completed trial counts, and p95/error
ranges separately for candidate confirmations and the predeclared failing
level, so different workload levels are never presented as one repeatability
range. These are descriptive ranges, not confidence intervals. It also includes
application and host telemetry coverage, and cgroup sample availability. The Markdown technical appendix also includes bounded observed
application-metric ranges grouped by campaign, trial, phase, metric, type, unit,
and label-key schema. It omits exporter sources and label values, caps output at
256 rows, and states how many rows were omitted. Values span observations and
label-distinct series; they are not summed or interpreted as causal proof.
Counter rows also show first-to-last deltas per stable label set; decreases are
counted as resets and excluded from those ranges. The executive report and JSON
assessment expose declared peak demand, growth, reserve, projected demand, and
the signed conditional arithmetic margin. Operational demand margin remains
absent unless an operational recommendation is established; a conditional
arithmetic surplus does not establish demand coverage.

The protocol p95 SLO uses complete journey latency, including configured waits
between requests. HTTP request p95 remains a separate diagnostic measure and
can be lower; reports preserve both measures and use journey p95 for protocol
decisions and level-specific ranges.
Campaign summaries, per-phase results, and measurement-quality findings each
show the campaign evidence digest inline so every technical row can be traced
directly to its saved source artifact.

A replicated breach at a predeclared failure level proves a breach only at that
tested workload. It does not establish the lowest failing level; interpret it
alongside the separately reported exploration campaigns, and do not interpolate
across gaps.

This is an experimental decision aid, not an operational certification. The
reported spread is not a confidence interval and does not account for temporal
dependence. `recommended_operating_users` remains absent unless a future
validated measurement envelope and workload-specific generator qualification
support it; current external application campaigns remain limited. A digest
alone cannot prevent someone from resealing a changed protocol, so use a
trusted external timestamp/signature process when an audit trail requires one.
The Markdown output contains the executive assessment and technical appendix;
the JSON bundle embeds the protocol, assessment, campaign artifacts, evidence
digests, and replay command. Its reproduction manifest includes a PowerShell
script that restores the embedded protocol and role-labeled campaigns to
relative files beneath `reproduction/`, plus an offline assessment command to
run from the root of a compatible Damocline source checkout. Replaying the
saved assessment does not repeat target traffic. Both outputs are written with owner-only file
permissions and may contain private target metadata, so keep them local and
gitignored.
