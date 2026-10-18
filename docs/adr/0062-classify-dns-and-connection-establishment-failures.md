# ADR 0062: Classify DNS and Connection Establishment Failures

## Status

Accepted for wrapped Go resolver and connection-establishment errors exercised by the local fixtures. Resolver subtypes, TLS certificate causes and remote network behavior remain outside this evidence.

## Context

The HTTP engine counted resolver failures and connection refusal under the same general transport-failure counter. That prevented reports from distinguishing a lookup failure from a failed TCP connection attempt. Existing timing traces showed DNS and connect callbacks, but their timing counts did not classify the terminal request outcome.

## Decision

Classify wrapped `*net.DNSError` outcomes as DNS failures and wrapped `*net.OpError` outcomes whose operation is `dial` or `connect` as connection-establishment failures. Check request and run timeouts before these classifications, and check TLS errors independently. A reset after a connection is established remains a general transport failure. DNS and connection counts are subcategories of `transport_failures`; they are not added to that total a second time.

Advance HTTP report schema to v20. Each current HTTP diagnostics ledger carries a failure-classification version marker, DNS and connection counters, and the general transport counter. Recompute and reader validation require the marker and reconcile per-step, aggregate and window counters with classified failure reasons. Schema-v1 through v19 reports remain readable; their missing category counts remain unknown rather than being inferred as zero.

## Consequences

The local native-engine fixtures independently induce NXDOMAIN and a refused loopback connection and assert exact DNS versus connection-establishment classifications. The existing reset fixture ensures an established-socket reset is not labeled a connect failure. CLI and HTML summaries expose the new counts. The classifications describe the error surfaced by the Go client and do not establish its root cause, whether an HTTP request reached the server, or application capacity. DNS timeout remains a timeout category; the current report does not distinguish DNS response subtypes such as NXDOMAIN and SERVFAIL.
