# ADR 0139: report connection establishment failures

## Decision

Emit a client transport finding for a positive connection failure count in
the schema-v20 or later versioned failure ledger. Use the existing count and
leave the report schema unchanged. Older ledgers cannot support this finding.

The signal describes the observed transport phase. The dial error alone does
not establish the underlying infrastructure cause or target receipt. HTTP
status evidence and reused-connection waits retain independent findings.

## Validation

The bounded native-engine loopback refusal fixture verifies that the finding
appears without an HTTP status or connection acquisition wait finding.
