# ADR 0168: Extend the opt-in envelope to open-arrival exploration and campaigns

Open-arrival exploration and campaigns can use the existing extended
envelope for an explicitly authorized lab or staging target. The request
ceiling, worker ceiling, and scheduled starts per second remain distinct.
Existing aggregate wall budgets and the longest-journey rate check still
apply. Generator guardrails stay mandatory.

Require a passing, fresh open-arrival self-calibration and exact worker,
rate, and workload matching through the extended calibration gate. Require
the configured request ceiling and timeout to match. Exploration searches
only calibrated points, including both requested bounds and any in-range
exact target. A campaign checks its base and spike before traffic. A
generator guardrail stop remains a generator result.

Extended artifacts use arrival exploration schema 3 and arrival campaign
schema 10, with an aggregate envelope checked against embedded runs by
offline readers. Standard artifacts keep their existing schemas and limits.
