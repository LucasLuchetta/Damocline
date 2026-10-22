# ADR 0109: Bind capacity decisions to phase-aligned host and application evidence

## Status

Accepted

## Context

A workload result alone cannot explain what the application and its dependencies experienced during each campaign phase. A one-time host snapshot, aggregated metric names, or metric samples detached from phase boundaries can obscure missing coverage and mix unlike series. Capacity conclusions also need a protocol declared before traffic so candidate confirmations and failure observations remain distinct and replayable.

## Decision

Add an opt-in, bounded Linux host sensor and preserve application metric samples as typed, label-distinct time series. Bind observations to campaign phases with explicit timestamps and coverage states. Persist the predeclared capacity protocol, separate exploration from independent candidate and failure campaigns, and recalculate the assessment offline from saved evidence. Reports use completed-journey latency for journey SLO decisions and retain HTTP request latency as a separate diagnostic. Incomplete telemetry, limited measurement quality, unqualified workload representativeness, or missing independent campaigns must withhold an operational recommendation.

The lab fixture is disposable and local. Sensor authentication, target identity, container scope, resource limits, generator qualification, and cleanup are explicit and bounded. Private campaign profiles and reports remain local and gitignored.

## Consequences

- Assessments can associate external workload outcomes with host, container, and application evidence over the same declared phases.
- Readers can distinguish absent samples from zero values, keep label-specific series separate, and replay the decision without sending traffic.
- The protocol can report observed passes and breaches without promoting limited evidence to a capacity recommendation.
- Collection adds bounded target and host overhead; shared-host fixtures and exploratory audits remain diagnostic and cannot certify a separate workload or production environment.