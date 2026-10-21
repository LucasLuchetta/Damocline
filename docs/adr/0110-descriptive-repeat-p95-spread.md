# ADR 0110: Describe request-p95 spread across campaign repeats

## Status

Accepted.

## Context

Campaign audits already expose each completed observation trial's request p95 and the minimum/maximum spread. A single extreme repeat can be hard to interpret from min/max alone. Requests within one phase are temporally and actor-correlated, so their count cannot be used as the independent sample size for repeat variability. Campaigns currently allow only two to five repeats.

## Decision

Closed-loop campaign schema v14 records a median and median absolute deviation (MAD) over one aggregate request p95 per completed observation phase, plus a descriptive band from `max(0, median - 3*MAD)` to `median + 3*MAD`, only when at least two observation phases completed. With fewer than two, no robust repeat summary is emitted. The campaign audit recomputes these values from saved observation phases and does not trust the persisted aggregates.

The method is labeled `MEDIAN_PLUS_MINUS_3_MAD`; its assumption is labeled `INDEPENDENT_OBSERVATION_PHASES_DESCRIPTIVE_ONLY`. The band is an outlier/spread summary, not a confidence interval or capacity bound. It does not account for sequential dependence, establish stationarity, or validate workload representativeness. It does not modify trial outcomes, the repeat pass-fraction interval, or the assessment verdict.

## Validation

Deterministic tests cover median/MAD arithmetic, rejection of a single completed phase and non-finite values, campaign persistence, offline recomputation and comparison of persisted fields from five trial summaries with an outlier, CLI rendering without an independence claim, and preservation of the separate conditional pass interval. Historical campaign schemas are not assigned synthesized values for schema-v14 fields.

## Consequences

Reports make repeat-level request-p95 variation easier to inspect while retaining the observed trial rows. With only two to five repeats the MAD band may be zero or unstable; readers must use the individual trials and recorded limitations. An incomplete campaign with only one completed observation shows no robust band, rather than implying zero spread. Cluster-aware uncertainty and a defensible experiment design remain open.
