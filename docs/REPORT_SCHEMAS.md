# HTTP run report schemas

## Supported versions

The engine currently emits schema v13. The `insights` reader accepts v1 through v13 and rejects unknown future versions. Historical reports are read into the current Go model without rewriting the original file; fields introduced after a report's schema version remain absent and are not evidence of a measured zero.

| Version | Fields introduced or versioned | Compatibility meaning |
| --- | --- | --- |
| v1-v2 | Base run metadata, configured workload, request and journey summaries. | Fields not present in a particular saved report remain unknown. |
| v3 | Generator runtime summary. | Missing generator observations are not zero consumption. |
| v4 | Observation horizon and generator process CPU/RSS evidence. | Wall-clock elapsed time and workload observation time are separate. |
| v5 | Weighted profile definitions and per-profile journey summaries. | Weights express the declared mix, not proof of representativeness. |
| v6 | Conditional IID-assumption P95 rank interval. | An absent interval is not a zero-width interval. |
| v7 | Per-step HTTP client phase counts and timing totals. | Fields describe client-observed phases and are unknown in older reports. |
| v8 | Exact status and structured HTTP outcome counters. | Missing historical counters are unknown, not zero failures. |
| v9 | Workload-mix provenance labels. | A label is declared provenance and does not validate a mix. |
| v10 | Generator sampler cadence, gap and collection cost. | Missing cadence and cost are unavailable evidence. |
| v11 | Opaque workload fingerprint. | A fingerprint is a declared-shape digest, not a signature or endpoint attestation. |
| v12 | Bounded response-body completion, interruption, size and oversize observations. | A recorded complete zero-byte response differs from a report that predates body observations. |
| v13 | Per-step request and explicit-profile completed-journey evidence in observation windows. | Missing or inconsistent current window ledgers are invalid; older reports cannot support windowed per-step conclusions. |

For schema-v11 and newer reports attached to calibration, workload-shape assessment recomputes the fingerprint only for the persisted single-step `GET /work` reference subset. Other shapes remain unverified because reports do not retain all request templates or values needed for safe reconstruction. A matching fingerprint is not a signature, endpoint attestation, or general workload-equivalence proof.

## Reader and fixture policy

Golden JSON fixtures in `internal/insights/testdata/reports/` cover every currently supported version. Tests verify reader acceptance, JSON round-trip stability, and preservation of a version-specific field at each schema milestone. Separate tests verify that unknown future versions are rejected and that schema-v11 absence of body fields is not reinterpreted as an observed zero-byte body.

No automatic migration rewrites historical artifacts. Consumers must use the report version to determine whether a field was recorded. A reader update must add support and a fixture for a new schema version before increasing the accepted ceiling.
