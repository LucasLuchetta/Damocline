# ADR 0182: Independent-evidence cross-check and k6 comparison

## Status

Accepted

## Context

Damocline conclusions must be falsifiable on representative applications.
They need evidence that Damocline did not produce, such as server access
logs and an independent generator.

## Decision

- `damocline crosscheck` reads a run report and an access log (`nginx`,
  `combined` or `json`) offline. It counts entries in the run window, widened
  by a declared clock tolerance, by method and path pattern. Scenario
  placeholders match one path segment, and `--path-map` handles proxy
  rewrites. It compares request, 2xx/3xx/4xx/5xx counts and server p95 with
  Damocline's counters. Damocline requests without a response only widen the
  upper bound. A server p95 above the client p95 is a mismatch. Matching
  entries outside the window flag clock skew, and a suspected skew prevents
  an overall MATCH. Other traffic in the window is reported, never
  subtracted.
- `lab/k6-compare/scenario-to-k6` converts only scenarios it can reproduce
  exactly (single closed-loop workflow). It refuses the rest. Header values
  are read from the environment so credentials never enter the script.
- `damocline compare-external` classifies the differences from a k6 summary
  as consistent, explainable (documented definition differences) or a red
  flag. A red flag means k6 fails a level Damocline approved, or k6 delivers
  materially more throughput at equal VUs.

## Consequences

Tolerances are declared defaults and appear in the output. The run report is
decoded without re-running the full artifact validation, so validate it with
`damocline report` first when provenance matters. Neither command sends
traffic or contacts the target.
