# ADR 0114: Record exact workload source identity

## Status

Accepted

## Context

Campaign phase configuration records a semantic workload fingerprint, but it does not identify the exact scenario or private profile bytes used to begin a run. Temporary scenario materialization for profile runs also removes the link to the saved profile. Reviewers can therefore compare recorded controls with a profile, but cannot establish that the profile file has remained byte-identical since the run.

## Decision

Closed-loop campaign schema v15 records an optional workload source descriptor. Direct scenario runs record the exact bounded scenario-file SHA-256 and scenario schema. Saved-profile runs record the exact bounded profile-file SHA-256, profile name and schema. Both record the validated semantic workload fingerprint. The digest is computed from the same bounded byte buffer that is decoded. The descriptor contains no filesystem path or source content and is copied to running checkpoints and final reports.

Offline campaign validation checks source descriptor type, source schema/name, SHA-256 syntax and consistency of the semantic workload fingerprint across all recorded phases. Profile verification compares the current saved profile's exact byte digest with the campaign descriptor when one is present. Historical campaigns and campaigns started directly from a scenario remain semantically verifiable but do not claim profile byte identity.

## Consequences

Reports identify whether the exact scenario/profile bytes have changed and audits expose that identity alongside phase evidence. SHA-256 is unkeyed: it does not prove who created either file, authenticate a campaign report, resolve environment-backed secrets, record target data state, or attest the application/host. The semantic workload fingerprint retains existing exclusions and limitations. Campaign schema readers continue to accept historical reports without this optional field.

## Validation

Unit and end-to-end localhost tests verify exact source digests, reject malformed descriptors and inconsistent phase fingerprints, verify profiles, reject edited-profile bytes, and assert offline behavior without sending traffic. Standard project gates are required before PR update.
