# HTTP run report schemas

## Supported versions

The engine currently emits schema v20. The `insights` reader accepts v1 through v20 and rejects unknown future versions. Historical reports are read into the current Go model without rewriting the original file; fields introduced after a report's schema version remain absent and are not evidence of a measured zero.

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
| v14 | Explicit `connection_policy` (`reuse` or `new_per_request`) applied by the engine and included in workload fingerprints. | A missing policy in an older report is unknown; only schema-v14 reports record this comparison dimension. |
| v15 | Optional controlled-reference evidence with independent server ledger counters, bound to the calibrator fixture fingerprint. | Accepted only for the calibrator-owned loopback soak; it does not attest arbitrary endpoints or external report authenticity. |
| v16 | External-cancellation count for planned arrivals not admitted before the schedule ended. | Required in v16, including when zero; absent in older schemas and never inferred as zero. Distinct from scheduler-late and worker-busy drops. |
| v17 | Count of arrivals admitted by the scheduler's worker handoff. | Required in v17, including when zero; absent in older schemas and never inferred as zero. Current arrival reports reconcile planned = admitted + dropped and admitted = started iterations. |
| v18 | Effective generator CPU capacity and source/known metadata. | Unknown entitlement is distinct from unlimited capacity and prevents positive quality. |
| v19 | Logical CPU count independently from GOMAXPROCS and effective CPU quota. | Missing or impossible logical CPU evidence is not inferred from other CPU fields. |
| v20 | Versioned HTTP failure classification with DNS-resolution and connection-establishment failure counters, retained as subcategories of transport failures. | The explicit classification version distinguishes measured zero counts from reports that predate the evidence. TLS and network timeouts remain separate categories; TCP resets after connection are not mislabeled as connection-establishment failures. |

For schema-v11 and newer reports attached to calibration, workload-shape assessment recomputes the fingerprint only for the persisted single-step `GET /work` reference subset. Schema-v14 reports record whether the engine reuses connections or opens a new connection per request. The fixed calibration covers reuse only; `new_per_request` is outside its envelope. Other request shapes remain unverified because reports do not retain all request templates or values needed for safe reconstruction. A matching fingerprint is not a signature, endpoint attestation, or general workload-equivalence proof. Schema-v15 and newer reports may report `VALID_WITHIN_ENVELOPE` only for the calibrator's own 30-second loopback soak when calibration, same-executable identity, generator health, response observations and independent server counters reconcile. This qualifies that controlled fixture execution only.

## Reader and fixture policy

Golden JSON fixtures in `internal/insights/testdata/reports/` cover every currently supported version. Tests verify reader acceptance, JSON round-trip stability, and preservation of a version-specific field at each schema milestone. Separate tests verify that unknown future versions are rejected, schema-v11 absence of body fields is not reinterpreted as an observed zero-byte body, and schema-v20 failure-classification metadata is required.

No automatic migration rewrites historical artifacts. Consumers must use the report version to determine whether a field was recorded. A reader update must add support and a fixture for a new schema version before increasing the accepted ceiling.
