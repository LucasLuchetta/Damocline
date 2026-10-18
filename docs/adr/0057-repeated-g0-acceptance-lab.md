# ADR 0057: Preserve repeated end-to-end G0 acceptance evidence

## Status

Accepted

## Context

The calibrator already runs the native engine against a separate loopback fixture and verifies exact workload profiles, a sustained soak, overload, induced HTTP failures, and independent fixture counters. Running it by hand did not provide one auditable acceptance decision across independent process executions, and early exits could leave logs or partial evidence scattered among local files.

## Decision

Add a bounded acceptance command that starts the calibrator as a child process for each independent repeat, requires two repeats by default, and runs the local HTTP transport fixture matrix. Verify the persisted calibration artifact directly: exact profile/rate/connection-policy coverage, every native-engine report and body ledger, aggregate and per-profile independent request, byte, and TCP connection counters, healthy-stage conservation, controlled-soak `VALID_WITHIN_ENVELOPE` recomputation, and negative overload/HTTP-failure controls. Record the expected and observed value for every gate.

Write an immutable evidence directory with state snapshots before and during each attempt, individual calibration and transport artifacts, SHA-256 artifact digests, and capped stdout/stderr logs streamed to disk while each child runs. A failed gate, process error, timeout, or interruption leaves a non-passing manifest and its available evidence. Reject an existing output directory so earlier evidence cannot be overwritten. Limit each calibration process to at most 240 seconds, the transport matrix to 30 seconds, each log stream to 1 MiB, and each JSON artifact to 16 MiB.

Recompute the persisted soak quality with the report package before accepting it. Report p95 min/max spread across repeats descriptively only; these sequential same-host runs do not establish environmental independence, a confidence interval, cross-host equivalence, or causal improvement. Keep the full multi-minute lab opt-in instead of adding another long calibration job to routine CI; CI continues to compile and unit-test the runner.

## Consequences

The project has one reproducible, fail-closed entry point for producing local G0 acceptance evidence, including failures and interruptions. A pass applies only to the fixture, generator binary, runtime, and windows in the manifest. It does not establish endpoint identity or external application capacity, does not validate untested workloads, and does not declare a universal generator limit.
