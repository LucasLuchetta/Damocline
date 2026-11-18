# ADR 0134: bind modified builds to complete executable identities

## Status

Accepted.

## Context

Generator identity accepted a VCS revision without checking whether the build
contained local modifications. Calibration also shortened commit and executable
digests and could fall back to a module version. These values could associate
different implementations with the same reference calibration.

## Decision

Use a full 40-character revision only when build metadata explicitly reports
`vcs.modified=false`. Otherwise hash the running executable with SHA-256 and
retain all 64 hexadecimal characters. If neither identity is available, record
an unavailable identity and keep calibration unverified. Module versions alone
do not identify implementation bytes. Do not record executable paths.

Historical report and calibration records remain readable. Shortened historical
runner identities do not match newly generated full identities, so users must
repeat calibration before comparing against the new runner.

## Validation and limitations

Deterministic tests cover clean, dirty, missing and malformed modification
metadata, unavailable executable digests, module-only metadata and full runner
identity retention. Existing artifact readers and runtime comparison tests
remain applicable. Digests are unkeyed and do not attest build custody or host
identity. A clean revision describes source metadata, not reproducible build
output or compiler configuration.
