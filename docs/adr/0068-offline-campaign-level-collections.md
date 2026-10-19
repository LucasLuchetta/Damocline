# ADR 0068: Aggregate offline campaign levels without interpolation

## Status

Accepted

## Context

One bounded campaign records repeated evidence for a single base workload
level. Capacity exploration records multiple short probes, but its results are
not the sustained phase evidence used by the offline campaign audit. Readers
therefore had no way to review several independently saved campaign levels in
one traceable artifact.

## Decision

Allow `damocline audit` to accept two or more repeated `--campaign` options.
Keep the existing single-campaign artifact shape unchanged. A collection
contains a complete audit per input and reports aggregate observed pass/fail
levels only when each audit is valid and the declared workload fingerprint,
target, environment, application/workload revisions, profile-mix declarations,
SLOs, timing, request controls, phase schedule, spike proportion, and repeat
count match. Inputs without schema-v14-or-later fingerprints and
connection-policy evidence, or without recorded workload, application, and
environment revisions, keep their individual audit results but receive
`NOT_COMPARABLE` collection state.

Demand is evaluated only from the campaign whose base level equals the exact
calculated target. Neighboring levels are not used for interpolation. A pass
at a higher level following a failure at a lower level is surfaced as
`NON_MONOTONIC`; it does not produce a capacity boundary. Other pattern labels
describe pass-below-fail, pass-only, fail-only, or no-decision observations.
Collections are limited to 20 files, 16 MiB per file, and 64 MiB total.

Campaign audits also retain per-trial and per-phase workflow breach details
when a named profile or aggregate journey exceeds its configured p95 or error
objective. An error count within budget is not treated as a breach, and invalid
evidence suppresses this diagnosis. The output attributes the measured SLO
crossing to a workflow, not its root cause.

## Consequences

Reviewers can trace each level to its source evidence and compare multiple
observations under matching declared controls. Matching declarations still do
not verify host identity, actual user-demand equivalence, statistical
dependence, stationarity, or sustainable capacity. The aggregate is an
offline summary of observed levels, not a capacity curve or certification.
