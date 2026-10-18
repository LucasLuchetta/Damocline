# ADR 0051: Calibrate exact workload shapes through the native engine

## Status

Accepted.

## Context

The local reference preflight previously measured one 4 KiB GET fixture and treated declared shape fingerprints as coverage. Request method, body size, and response size can materially change generator cost, so declaring a shape without exercising it through the scheduler and HTTP engine is insufficient evidence.

## Decision

Add a fixed, bounded schema-v3 profile matrix executed through the native Damocline engine against a separate loopback fixture process. Cover GET response bodies of 0, 1, 4, and 64 KiB and POST with a synthetic 4 KiB request body and a 4 KiB response. Each profile uses exactly 10 open-loop arrivals/s, 10 workers, `max_rps=50`, reusable HTTP connections, a 5-second observation, and a 1-second timeout. Record shape fingerprints and client/server request and byte counters, then require exact reconciliation before a passing preflight can be emitted. Scenario coverage matches the full tested tuple and never resolves environment-backed request-body content offline.

## Consequences

Only these request shapes and exact control tuples gain local matrix coverage. The evidence is not a cross-host generator guarantee, TLS or new-connection qualification, variable response distribution, multi-step journey validation, endpoint identity proof, or application-capacity certificate. Schema-v1 and v2 preflight artifacts remain readable but cannot claim schema-v3 matrix coverage. The longer bounded calibration continues to fit the existing hard deadline; local and CI runtime validation must verify that bound on Windows and Linux.
