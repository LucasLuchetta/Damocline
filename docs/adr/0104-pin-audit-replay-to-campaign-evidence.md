# ADR 0104: Pin audit replay to campaign evidence

## Status

Accepted

## Context

The Markdown capacity report carries content-addressed IDs for its source campaign and phase reports, but its replay command previously only named the source path. A campaign file could change after the report was produced and the command would silently assess the replacement. Reviewers had to compare IDs manually, and multi-campaign reports needed to keep each digest paired with the corresponding source path.

## Decision

Add repeatable `audit --expect-evidence-id sha256:<digest>` and `audit --expect-campaign-file-sha256 sha256:<digest>` arguments. When supplied, there must be exactly one valid lowercase SHA-256 digest of each kind per campaign path, in argument order. `audit` decodes and hashes the same bounded source bytes, then rejects any mismatch before writing JSON or Markdown output. Generated single-campaign and collection replay commands include both expected digests.

## Consequences

The report's replay command checks both that each source still decodes to the same evidence fields and that its bytes exactly match those reviewed before reproducing the offline assessment. Any mismatch fails closed without creating assessment artifacts. The expected digests also disambiguate source mappings in multi-level collections. Deterministic tests cover byte-only changes with stable semantic IDs, fail-closed output handling, and digest-to-source ordering.

The semantic evidence ID covers canonical JSON of the decoded campaign struct. The additional file digest covers exact input bytes, including whitespace and fields unknown to the reader. Both are unkeyed identifiers, not signatures or immutable archives. They do not prove who created the campaign, when it was created, or whether its claims about target, host, environment, and workload are true. A person who can replace both source and report can calculate matching new digests. Full workload replay still depends on the private scenario/profile and external state; retain the source bytes with the assessment.
