# ADR 0075: Configurable generator resource abort guardrails

## Status

Accepted

## Context

Campaigns need a bounded way to stop offered work when the local generator exceeds an operator-selected resource limit. A generator limit is operational safety evidence and must not be presented as a target application capacity limit or a root-cause diagnosis.

## Decision

Closed-loop and open-arrival campaigns accept optional maximum generator process CPU percentage and RSS MiB limits. A configured limit must be exceeded for a bounded number of consecutive process samples (2–20, default 2) before the active run is cancelled. CPU uses the monitor's detected generator CPU capacity; RSS is process RSS. The first trigger, observed value, limit, elapsed time, and sample count are persisted with the run. Campaign artifacts persist the policy, and offline assessments require each phase's run evidence to match it. A triggered phase is ABORTED; short or incomplete delivery cannot support an application PASS/FAIL conclusion.

## Consequences

Run report schema advances to v23, closed-loop campaign schema to v7, and open-arrival campaign schema to v3. The guardrails are disabled by default. Sampling cadence may miss short peaks; these limits do not measure host-wide or target resources. No causal claim follows from a trigger.
