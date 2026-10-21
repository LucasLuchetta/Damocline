# ADR 0106: Persist path-free generator runtime provenance

## Status

Accepted

## Context

A campaign can compare measured phases only when the load generator's runtime and effective CPU environment are known to be comparable. Run reports already record Go runtime health and CPU entitlement, but they do not persist an identity for the runtime and executable build. Offline reports therefore cannot tell whether phases ran on the same build. A local filesystem path would create privacy leakage and would not be a portable identity.

## Decision

Run report schema v31 records OS, architecture, Go version, and a path-free runner identity. The identity is the full source commit when Go build metadata provides it, otherwise the complete executable SHA-256, otherwise a module version; the method and value are explicit. Unsupported identity is represented as unavailable and fails closed for v31 measurement quality.

Offline campaign audit exposes per-phase identity, logical CPUs, GOMAXPROCS, effective CPU capacity and its evidence source. It marks missing legacy provenance UNKNOWN, changed phase identity/capacity inconclusive, and matching values CONSISTENT. It keeps physical host model and target identity explicitly unknown.

## Consequences

- Saved current run evidence can identify the runner build without storing local paths.
- Replaying campaign assessments can surface changes in runner build and effective CPU capacity.
- Existing schemas remain readable and are not assigned invented identity.
- This does not attest host hardware, operating-system image, target endpoint, or workload representativeness.
- Reports include the complete runner commit or executable digest; these values are not credentials.
