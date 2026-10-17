# ADR 0015 — Extend reference evidence without overstating capacity

## Status

Accepted (experimental); all referenced G0 gates retain their full exit criteria.

## Context

The existing separate-process calibration validates one fixed 4 KiB, 20 ms plain-HTTP loopback reference. G0-02 conservatively classifies different workloads as inconclusive. An additional transport matrix is useful for correctness tests but must not be confused with generator scheduling or server capacity.

## Decisions

1. Introduce a separate, short, loopback-only HTTP **transport** matrix for known response sizes, delayed responses, streaming, explicit HTTP 503, keep-alive enabled/disabled, and fixture-only TLS. Its JSON schema and success label are explicitly separate from the load-scheduler calibration. Every case has an independently reconciled client/server request ledger and bounded response size, request count and time. Add separate native-engine regression tests for fixed 1/4/64 KiB HTTP responses, latency tails and closed connections against an independent server ledger, within a deliberately small 5-arrivals/s, two-second stage. A cancellation fixture also verifies one interrupted client request against server-side `received`, `completed`, `cancelled`, and `rejected` counters.
2. Extend the **offline** scenario coverage report with calibration age, runtime compatibility and host-identity uncertainty. An operational 24-hour review threshold and five-minute future-clock guard flag unsuitable artifacts. OS, Go and CPU equivalence are weak compatibility signals, **not** evidence that the same machine ran both workloads. Runner source revisions may be unknown; absence is not equality.
3. Add request/report **data quality** separately from SLO outcomes. Impossible counters are INVALID; missing generator evidence, incomplete arrivals or no equivalent calibration are LIMITED. VALID_WITHIN_ENVELOPE remains reserved and must **not** be emitted before a defensible workload-specific preflight and authenticated host evidence are available. An invalid ledger must not retain an observed PASS.

## Safety

All new network traffic is restricted to ephemeral httptest loopback servers, with no application addresses, third-party requests, proxies, credentials, user identities, production paths or databases. Bounds are fixed in source; existing Damocline traffic caps remain unchanged. The TLS fixture trust context is local to its ephemeral self-signed test server and is not a recommendation to disable TLS verification for remote workloads. Reports contain only named public fixture cases, counters and generic findings.

## Explicit limitations / remaining work

- The optional CLI transport matrix uses standard Go HTTP clients, **not** Damocline's native load-scheduling and delivery loop. Separate unit/integration regressions exercise the native loop for a few new HTTP reference shapes, but do not calibrate its maximum throughput, remote latency accuracy or full statistical delivery envelope.
- The cancellation ledger fixture verifies a single interrupted request on an ephemeral loopback server. It does not establish conservation across every failure mode, campaign phase, or graceful-drain boundary.
- DNS/connect errors, true variable-size distributions, arbitrary server body behavior, HTTP/2, and remote TLS remain outside the measured envelope.
- A 24-hour threshold is a safety review heuristic, not a statistically justified calibration expiry.
- The v1 preflight has no host identity attestation, cross-host clock offset or workload-equivalent fingerprint. It cannot approve a target.
- Quality fields are additive in the current report version; historical reports lack these fields, which should be treated as unknown.
- G0-03, G0-08, G0-16 and the complete G0-02 decision gate remain **open** until their full acceptance evidence exists.
