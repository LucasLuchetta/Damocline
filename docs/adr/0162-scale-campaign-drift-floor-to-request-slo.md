# ADR 0162: Scale the closed-loop campaign drift floor to the request SLO

New closed-loop campaigns default to an absolute drift threshold of
`max(5 ms, 1% of p95_limit_ms)`. A 1,000 ms objective yields 10 ms.
One percent keeps the threshold small relative to the request budget.
The 5 ms minimum preserves the prior noise floor for objectives up to
500 ms. This is a decision heuristic, with no noise calibration implied.

The relative p95 and throughput thresholds and independently enforced
request and journey SLOs keep their existing meanings. An explicitly
selected threshold, including zero or 5 ms, takes precedence. The existing
campaign policy field persists the effective value. Offline readers keep
using the saved policy, or the historical fixed default for legacy files.
