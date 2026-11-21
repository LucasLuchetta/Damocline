# ADR 0174: Offline before/after campaign comparison

## Status

Accepted.

## Context

Users want to know whether a code change altered latency, errors or
throughput of a workflow under the same load. Comparing two single numbers
invites false conclusions: run-to-run spread can exceed the change, and two
campaigns with different scenarios, load levels, envelopes or pass criteria
are not comparable at all.

## Decision

`damocline compare --before X --after Y` reads two saved closed-loop campaign
artifacts (a `damocline capacity` output directory, using its
`campaign.json`, or a campaign JSON file) through the existing bounded
campaign reader. It sends no network requests.

- Blocking settings: scenario, workload semantic fingerprint (when both
  record one), base and spike users, phase durations, load envelope,
  objective contract, stability policy and run pass criteria. Any difference
  marks the result `NOT_COMPARABLE`, omits all deltas and exits with code 1.
- Non-blocking settings (target, declared environment, repeats, soak) are
  flagged but do not prevent the comparison.
- Only observation phases of non-aborted trials are used. Per repetition it
  extracts request p95, error rate and throughput overall and per step.
- Each metric reports median, min, max and the per-repetition values on both
  sides. `IMPROVED`/`REGRESSED` requires at least two repetitions per side
  and non-overlapping ranges; otherwise the verdict is `INCONCLUSIVE`
  (within run-to-run spread). Identical values are `UNCHANGED`.
- Measured facts and labelled hypotheses are separate sections. Outputs are
  Markdown on stdout, optional `--report-out` Markdown and `--out` JSON;
  neither overwrites an existing file. Source labels use base names only.

## Consequences

The range-overlap screen is conservative and descriptive, not a significance
test; with few repetitions most real but small changes stay INCONCLUSIVE.
Open-arrival campaigns and spike/recovery/soak phases are not compared yet.
A comparison never attributes a change to its root cause.
