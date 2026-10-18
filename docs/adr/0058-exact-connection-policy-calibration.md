# ADR 0058: Calibrate exact HTTP connection policies

## Status

Accepted

## Context

The schema-v4 workload matrix measured five HTTP shapes at three exact arrival rates, but only with reusable connections. The engine supported `new_per_request`, while offline coverage correctly treated it as unmeasured. Comparing only shape and rate did not establish whether connection setup costs limited the generator for that policy.

## Decision

Schema v5 preserves the schema-v4 matrix and adds the same five shapes at exactly 2, 5, and 10 arrivals/s using `new_per_request`. Reuse profiles run for five seconds; new-connection profiles run for two seconds to keep the entire bounded calibrator under its existing deadline. The artifact records policy, rate, duration, timeout, worker count, rate cap, request shape, and independent loopback server request/body-byte counters for each profile. The aggregate server reconciliation derives expected requests from each profile's duration; an initial runtime attempt exposed and fixed a five-second-only aggregate assumption. Archived schema-v3 and schema-v4 artifacts retain their original profile matrices and validation rules.

Offline comparison matches connection policy and duration only when the full declared control tuple selects a measured profile. It still assesses arrival rate, response-shape distribution, transport, host identity, and endpoint identity independently. It does not interpolate rates or treat the profile matrix as evidence for an external application.

## Consequences

The local reference envelope can distinguish two supported HTTP connection policies for the fixed shapes and exact tested rates. The new profiles are shorter and provide fewer arrivals than their reusable counterparts; they are suitable for deterministic reconciliation and coarse generator screening, not tail-latency confidence or sustained capacity claims. Calibration currently records HTTP request and byte counts but not an independent per-profile TCP connection count, so the policy's network behavior is not separately attested by this artifact. Runtime validation remains limited to environments where the acceptance lab has been executed.
