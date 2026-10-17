# ADR 0013: Version the loopback preflight evidence

## Status

Accepted as an experimental reference-evidence format.

## Context

The separate-process local fixture checks timing and arrival accounting, but its top-level schema did not record a sufficiently complete, stable description of the generator and fixture to compare one run with another. A smoke pass cannot certify application capacity or a workload different from the fixed fixture.

## Decision

Embed a `preflight` object with its own schema version into the existing calibration report. The record includes bounded generator build/environment information, UTC observation boundaries, a canonical SHA-256 fingerprint of hardcoded non-secret fixture parameters, and per-healthy-stage offered/delivered counts. Reject unsupported preflight versions and incoherent metadata. The highest passing tested arrival rate is populated only after **all** fixture checks pass, not inferred from a failed or shorter run.

The fingerprint is an identifier, **not** a signature, trust anchor, host-proof or customer-scenario fingerprint. A matching fixture fingerprint alone cannot establish equivalent runtime conditions, elapsed freshness, generator headroom, or performance validity.

## Safety and limits

Do not store target hostnames, resolved fixture ports, authorization headers, environment contents, request/response bodies, or arbitrary scenario names in this record. Keep output in the existing private calibration report path. Do not expand traffic ceilings or relabel the smoke result as a scientific certification.

## Deferred

G0-02 and G0-08 will compare *requested real workloads* against independently validated envelopes, host identity, age and measured delivery; until then no preflight record may gate a positive application-capacity conclusion.
