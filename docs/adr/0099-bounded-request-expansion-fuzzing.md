# ADR 0099: Fuzz bounded request expansion

## Status

Accepted.

## Context

Request-template substitution and query encoding accept dynamic response values. Deterministic boundary tests cover known expansion cases, but arbitrary byte strings can exercise escaping and size-accounting edges that example tests do not enumerate.

## Decision

Add Go fuzz targets for bounded interpolation, query escaped-byte accounting, and expanded URL resolution. Successful interpolation must match regular-expression substitution and remain within its supplied byte budget; missing variables and oversized output must fail without returning partial output. Query-size accounting must match `url.QueryEscape`. For paths in the origin-relative form accepted by scenario validation, successful expanded URLs must stay under the URL cap and retain the configured scheme and host.

Run each target for a fixed five-second smoke window in Linux CI after the race-enabled test suite. Keep deterministic seed cases in the regular test path so every platform checks the core properties without requiring fuzz workers.

## Consequences

CI adds a bounded fuzz regression screen and may discover counterexamples in future changes. The short duration and current target scope do not establish exhaustive correctness, aggregate worker-memory bounds, or coverage of headers, bodies, report decoding, configuration parsing, and process interruption. Those remain separate G0/Q validation work.

## Validation

Run each fuzz target locally with a bounded duration and run the complete unit, vet, and build checks. Fuzz inputs and generated artifacts must remain synthetic and local.
