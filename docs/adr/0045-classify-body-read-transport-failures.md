# ADR 0045: Classify body-read transport failures

## Status

Accepted.

## Context

The engine reports a truncated response as a failed `body_read` outcome and records an incomplete-body observation. The structured HTTP diagnostics did not increment `transport_failures` for this failure after a successful response status, leaving the report's failure categories incomplete.

## Decision

Treat non-timeout `body_read` errors as transport failures in the structured HTTP diagnostic ledger. Preserve the HTTP status code and incomplete-body observation as separate overlapping dimensions. Timeouts remain in the dedicated timeout category; TLS handshake failures remain in the dedicated TLS category.

## Consequences

Truncated responses now appear in both `transport_failures` and `body_incomplete_count`, while retaining any received HTTP status. These counters describe overlapping observations and are not mutually exclusive outcome buckets. This does not identify whether the truncation originated in the target, an intermediary, or the network.
