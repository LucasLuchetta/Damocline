# ADR 0034: Record exact workload shape in generator calibration

## Status

Accepted (experimental).

## Context

The loopback preflight measured one fixed HTTP journey, while offline checks compared only selected scenario controls. A declared scenario could therefore match the rate and simple request-mix fields while differing in the exact path or other journey shape. A rate match alone cannot establish equivalence.

## Decision

Calibration record schema v2 stores an opaque SHA-256 fingerprint of the workload definition executed by healthy reference stages. It covers profile and step names, weights, methods, path templates, declared headers and assertions, environment-backed body length, extraction declarations, cookie-session count, and synthetic-data declaration. It excludes target identity, load controls, resolved secret values, response content, extracted values, and generated identifiers. The canonical preflight workload uses one GET step named `work` at `/work` with no assertions, body, headers, session cookies, or synthetic identifiers.

Scenario assessment and schema-v12 report quality recompute this fingerprint and require an exact match. Legacy calibration schema v1 remains readable, but its workload-shape dimension is `UNVERIFIED`. Any mismatch or non-assumption weighted mix is outside the fixed reference.

## Consequences

An exact match establishes equality of declared workload shape only. It does not prove that a run contacted the controlled fixture, that responses had the calibrated size or timing distribution, that connections were reused equivalently, or that the generator had sufficient headroom for the workload. Overall run quality remains `LIMITED`; no positive `VALID_WITHIN_ENVELOPE` claim is enabled by this ADR. The hash is not a signature or machine identity attestation.
