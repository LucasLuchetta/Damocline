# ADR 0112: Pin capacity audit replay to the auditor build

## Status

Accepted

## Context

Capacity assessment replay already pins source campaigns to both their decoded
evidence IDs and exact input bytes. The assessment did not identify which
Damocline executable interpreted them. A later build could apply changed audit
rules to identical source bytes without the report showing which implementation
produced the original verdict.

## Decision

Assessment schema v6 and campaign-collection schema v4 record the CLI version
and SHA-256 of the exact auditor executable. The digest is computed by bounded
streaming without persisting the executable path. Generated PowerShell replay
commands pass `--expect-auditor-sha256`; when supplied, the CLI compares the
running executable before reading campaign inputs or creating assessment
outputs. Campaign and executable digests remain separate from the semantic
evidence ID.

## Consequences

- A report identifies the implementation version and exact executable bytes
  that produced its audit.
- Exact replay fails closed when the current executable differs. An engineer
  may build or locate the recorded binary, or deliberately run a fresh audit
  under a newer version and retain it as a new assessment.
- The version label and executable hash do not authenticate the build origin,
  signer, source repository, or distribution channel. They do not make reports
  immutable or establish campaign claims about target, host, or workload.
- The auditor hash is not included in the campaign semantic ID; it describes
  the interpretation tool rather than the campaign evidence.
