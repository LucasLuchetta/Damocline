# ADR 0104: Pin audit replay to campaign evidence

## Status

Accepted

## Context

The Markdown capacity report carries content-addressed IDs for its source campaign and phase reports, but its replay command previously only named the source path. A campaign file could change after the report was produced and the command would silently assess the replacement. Reviewers had to compare IDs manually, and multi-campaign reports needed to keep each digest paired with the corresponding source path.

## Decision

Add repeatable `audit --expect-evidence-id sha256:<digest>` arguments. When supplied, there must be exactly one valid lowercase SHA-256 evidence ID per campaign path, in argument order. `audit` recomputes each campaign's evidence ID and rejects any mismatch before writing JSON or Markdown output. Generated single-campaign and collection replay commands include those expected IDs.

## Consequences

The report's replay command now checks that each source still decodes to the same evidence fields reviewed before reproducing the offline assessment. A changed interpreted field fails closed without creating assessment artifacts. The expected IDs also disambiguate source-to-digest mapping in multi-level collections. Deterministic tests verify IDs remain stable over identical assessment serialization and change when interpreted campaign content changes.

The digest covers the canonical JSON representation of the decoded campaign struct, not original file bytes or unknown JSON fields. Whitespace-only changes and fields unknown to the reader are outside this evidence identity. It is an unkeyed identifier, not a signature or immutable archive. It does not prove who created the campaign, when it was created, or whether its claims about target, host, environment, and workload are true. A person who can replace both the source and the report can calculate a new matching digest. Full workload replay still depends on the private scenario/profile and external state; archive the original bytes separately if byte-level custody is required.
