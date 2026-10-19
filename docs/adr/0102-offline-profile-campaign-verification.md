# ADR 0102: Offline campaign profile verification

## Status

Accepted

## Context

Private campaign profiles make workload declarations and campaign controls reusable. A saved campaign report contains the phase-level run configuration and campaign schedule produced during execution. Engineers need a bounded way to check that these declarations reconcile without repeating the workload against a target.

## Decision

Provide `damocline campaign profile verify --name NAME --campaign FILE [--out FILE]`. The command reads a local profile and a bounded saved campaign report, validates both, reconstructs the declared scenario using the current environment, and compares campaign controls, schedule, and every persisted phase's normalized run configuration. It creates no HTTP client and sends no network traffic. The optional JSON result is created exclusively with restrictive permissions and is never allowed to overwrite a file.

The result distinguishes `MATCHED`, `MISMATCHED`, and `UNVERIFIABLE`. A mismatch identifies the configuration field or phase without exposing secret values. Missing environment-backed inputs that prevent reconstruction produce `UNVERIFIABLE`.

## Consequences

A match establishes consistency between the selected profile and the configuration persisted in the selected report. SHA-256 identifiers help identify the exact bytes represented by canonical serialization, but they are not signatures or authentication. The check does not attest who created either file, the physical host, application revision, target identity, target data state, credential/account identity, or application-metrics endpoint settings. Environment-backed body contents are not represented in the workload fingerprint; only their configured length is. Engineers must preserve those external conditions and the original files separately to reproduce a result.

No campaign report schema change is required. The comparison uses the engine's shared normalized run-configuration projection so report construction and offline verification stay aligned.
