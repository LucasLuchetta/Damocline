# ADR 0069: Optional bounded final campaign soak

## Status

Accepted

## Context

Campaigns already repeat independent observation phases and may run a spike and recovery. A separate sustained stage was missing, while open-ended or multi-hour tests would violate the current local safety envelope.

## Decision

Add an optional 10-second to 3-minute `soak` phase at the configured base workload, only after all independent trials and their recovery phases pass. Persist it in campaign schema v4. It uses the native bounded run engine, existing full-journey and per-window SLO assessment, and the coarse campaign drift screen. Reserve a bounded phase drain in the campaign wall-time budget and retain the existing 15-minute total cap. Historical campaign schemas remain readable under their prior schedules.

## Consequences

The soak adds useful bounded repeated-load evidence, but does not establish statistical stationarity, detect every transient or slow leak, qualify multi-hour demand, or certify sustainable application capacity. Controlled drifting-fixture coverage and longer validated soaks remain open roadmap work.
