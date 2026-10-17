# ADR 0029: Preserve detailed loopback calibration failures

## Status

Accepted.

## Context

The bounded loopback calibration artifact retained failed checks, but its
command-line error only reported the number of discrepancies. A CI failure
could not distinguish missed or dropped arrivals, timing outside the fixture
envelope, insufficient generator observations, scheduler lag, or disagreement
with the independent server ledger without a local artifact that CI may not
retain.

## Decision

Calibration failures include bounded diagnostic strings for each failed
evidence dimension, with the expected and observed values needed to understand
the result. Stage checks separate arrival accounting, request outcomes,
journey outcomes, fixture outcome, latency envelope, generator health, and
scheduler lag. Aggregate server-ledger mismatches show expected and observed
counters. Non-finite latency measurements fail the latency gate.

Diagnostics describe only the program-owned loopback fixture and numeric run
fields; they do not include scenario URLs, credentials, response bodies, or
customer identifiers. A failed measurement remains a failed calibration; the
extra detail does not retry, relax, or reinterpret a gate.

## Consequences

Local and CI failures can be classified from the command output while retaining
the full report artifact for investigation. This improves diagnosis but does
not make shared-runner measurements repeatable, establish a generator ceiling,
or qualify any application workload.
