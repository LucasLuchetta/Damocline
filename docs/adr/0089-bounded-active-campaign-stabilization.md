# ADR 0089: Add bounded active campaign stabilization

## Status

Accepted (experimental). Passing a coarse drift screen does not prove stationarity or sustainable capacity.

## Context

Closed-loop campaign schema v10 and open-arrival campaign schema v6 supported warm-up, observation, spike, recovery and an optional final soak. Their drift screen only ran on observation and soak, so a workload could enter the decision-bearing observation before the operator had explicitly required a stable-window screen at the base load.

## Decision

Add an optional `--stabilization` phase before observation to both `campaign` and `arrival-campaign`. When enabled, it runs exactly once per trial at the configured base VU count or scheduled journey-start rate. The phase duration is 10 seconds through 3 minutes in the default campaign envelope and up to 30 minutes in explicitly enabled long-run staging mode. It is required evidence: observed SLO failure makes the trial fail; invalid or incomplete delivery makes it inconclusive; a passing run must also satisfy the persisted coarse drift policy. If windows do not meet the policy, the trial is inconclusive and observation does not start. There is no automatic retry or unbounded wait.

Closed-loop campaign schema advances to v11 and open-arrival campaign schema to v7. Both persist the configured duration, including `0s` when disabled, and include its phase and bounded drain in the wall-time budget. A saved `0s` means the stage is disabled; an omitted field also means disabled in schema v11/v7 for compatibility with callers that do not enable it. Offline campaign audit, exact-demand assessment, and report export validate and retain the phase. Historical schemas continue to use their original schedules and budget interpretation. The phase uses the same configured workload and engine controls as the following observation, with fixed window boundaries in each native run.

## Consequences

Operators can explicitly require short, bounded evidence of coarse stability before observation. This does not adaptively wait for stability, detect every transient, establish statistical stationarity, qualify memory leaks, prove generator/endpoint equivalence, or certify application capacity. The phase adds bounded traffic and time when enabled. Local deterministic fixtures verify phase order, drift and cancellation stops, dry-run schedules, budget accounting, and offline replay; they do not qualify arbitrary targets.
