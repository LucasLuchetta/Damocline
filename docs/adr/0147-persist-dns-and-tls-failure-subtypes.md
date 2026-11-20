# ADR 0147: persist DNS and TLS failure subtypes

## Status

Accepted.

## Context

Schema v20 introduced DNS-resolution and connection-establishment counters,
and `tls_failures` already existed, but neither DNS nor TLS failures recorded
which client-observed error class occurred. NXDOMAIN, resolver SERVFAIL,
untrusted certificates and a plaintext server behind an `https://` URL were
indistinguishable in persisted reports.

## Decision

Run report schema v34 adds an optional-in-JSON object
`http_diagnostics.failure_subtypes` to every `MetricSet` HTTP ledger
(aggregate, named step, observation window and window step):

- DNS: `dns_not_found` (`net.DNSError.IsNotFound`), `dns_timeout`
  (`IsTimeout`, a resolver-internal timeout that did not expire the request
  deadline), `dns_temporary` (`IsTemporary`, for example SERVFAIL), `dns_other`.
- TLS: `tls_certificate` (certificate verification / x509 errors),
  `tls_non_tls_response` (a plaintext HTTP record received by the TLS client;
  net/http replaces the typed record-header error with a fixed message, which
  is matched), `tls_other`.

Reader rules:

- v34: the DNS subtypes must sum exactly to `dns_failures` and the TLS
  subtypes to `tls_failures`, with non-negative values, in every ledger, and
  step/window sums must reconcile with their aggregates. Absence is accepted
  only when both totals are zero, because the partition is then fully
  determined; any non-zero total without subtypes is rejected.
- v1-v33: subtypes must be absent. Their subtype distribution is unknown and
  is never presented as zero.

A plaintext response to a TLS client is now classified as a TLS failure
(`tls_non_tls_response`) rather than a generic transport failure.

The failure-classification marker stays at version 1; the schema version
gates the new field. No file-rewriting migration is performed.

## Consequences

- Subtypes describe the Go client error surface only. They do not identify
  which resolver, zone, certificate authority or network element caused the
  failure, and a TLS failure never proves an HTTP request reached the server.
- A request-deadline expiry during resolution remains a network timeout, not
  a DNS subtype, matching the existing schema-v20 classification.
- TLS alert causes (for example a required client certificate) are grouped in
  `tls_other`; finer alert subtypes are not persisted.
