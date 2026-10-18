# ADR 0055: Calibrate exact workload shape and rate pairs

## Status

Accepted

## Context

The schema-v3 calibration measured five exact request shapes at 10 arrivals per second. Separate healthy stages measured 2, 5, and 10 arrivals per second only for the 4 KiB GET fixture. Shape coverage could therefore not support a claim that another payload shape had been exercised at the lower rates.

## Decision

Schema v4 records each of five fixed request shapes at each of three exact rates: 2, 5, and 10 arrivals per second. Every pair runs through the native HTTP engine for five seconds and records client scheduling/body evidence plus independent loopback server request and byte counters. No rate interpolation is allowed. Schema-v3 records remain readable and retain only their original single-rate matrix.

The calibrator deadline increases from 160 to 220 seconds to bound the expanded matrix together with the existing repeated/extended stages and soak. The child fixture remains alive for at most 210 seconds, leaving time for the parent to collect its final counters and shut it down. Reference routes remain unique per request shape; rate is controlled by separate engine runs.

## Consequences

Preflight schema v4 compared exact low-rate combinations across the five declared request shapes using reusable connections. Schema v5 adds the same combinations for `new_per_request`, with two-second profile windows. This does not validate intermediate rates, response distributions, TLS, other body sizes, multi-step journeys, per-profile TCP connection counts, hardware classes, or remote endpoint identity. The deadline is an execution limit, not a throughput or capacity recommendation.
