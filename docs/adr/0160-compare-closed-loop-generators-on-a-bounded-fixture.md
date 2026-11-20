# ADR 0160: Compare closed-loop generators on a bounded fixture

## Status

Accepted.

## Context

A second HTTP generator can expose discrepancies in delivered traffic and
latency. Changing the offered workload or observation boundaries at the same
time can make these discrepancies hard to interpret.

## Decision

Run Damocline and k6 serially against the existing loopback queue fixture at
fixed VU levels. Match journey structure, think time, request timeout, nominal
load duration, rate ceiling, redirect behavior and connection reuse settings.
Preserve both raw reports, including failures. Document timing and drain
differences alongside the comparison.

## Consequences

The exercise can reveal generator differences under controlled queue pressure.
Closed-loop pacing reduces offered traffic when responses slow down, so the
exercise cannot estimate an independent arrival-rate ceiling or calibrate
another workload.
