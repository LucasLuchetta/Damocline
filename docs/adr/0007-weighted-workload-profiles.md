# ADR 0007: Weighted workload profiles are explicit scenario inputs

## Status

Accepted.

## Context

One fixed sequence of HTTP steps cannot represent an application whose users perform different activities. Capacity conclusions require a declared workload mix, but random profile sampling makes short bounded tests hard to reproduce and can hide an unrepresented workflow.

## Decision

A scenario may contain either legacy top-level `steps` or two to ten named `profiles`, never both. Each profile has a positive integer weight, its own steps, and an optional think time. Closed-loop VUs receive deterministic smooth weighted-round-robin profile assignments for the run. Arrival mode selects the profile for each planned journey start with the same deterministic algorithm.

Reports preserve configured weights and record per-profile completed and cancelled journeys. Closed-loop reports also record the number of assigned VUs. Global step names remain unique so per-step metrics cannot merge unrelated operations.

## Consequences

Weights are an explicit synthetic assumption, not a statement about production behavior. Users must derive and review them from sanitized usage evidence. The feature does not provide response-driven variables, synthetic-data setup or cleanup, browser execution, or a partly open session model.

ADR 0024 adds bounded, self-declared provenance labels for a mix. These labels do not establish representativeness or validate source evidence.
