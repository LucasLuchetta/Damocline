# ADR 0040: Record and apply HTTP connection policy

## Status

Accepted (experimental).

## Context

The HTTP engine always reused keep-alive connections, but scenarios, workload fingerprints and reports did not record this condition. Calibration equivalence could therefore not distinguish workloads requiring a new connection per request. The current preflight exercises connection reuse only.

## Decision

Add a bounded `connection_policy` scenario option with `reuse` as the default and `new_per_request` as the alternate. Apply the alternate by disabling keep-alives on the shared Go transport. Include only the non-default value in the workload fingerprint so existing default reuse fingerprints remain stable. Persist the selected value in report schema v14. At this decision, only `reuse` was covered by calibration; schema v5 later adds an exact `new_per_request` profile matrix. Require schema-v14 reports for declared-run comparisons so older reports with unknown connection policy are not treated as equivalent evidence.

## Consequences

The engine can execute and report two explicit transport policies, and local server connection-state tests verify each policy under a bounded workload. This ADR did not qualify their comparative capacity; calibration schema v5 adds exact loopback workload profiles for both, while leaving independent per-profile TCP connection counts unrecorded. Schema-v1 through v13 reports remain readable, but comparisons involving them are not considered equivalent because their connection policy is unknown.
