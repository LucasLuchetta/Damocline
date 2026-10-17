# ADR 0004 — Evidence-gated capacity statements

**Status:** proposed

## Context

Damocline's main user needs answers about customer demand, workload-specific capacity and infrastructure alternatives. An HTTP PASS at one VU count does not prove sustainable capacity, and an SLO failure does not prove that a larger server is required. Scientific results are conditional on model assumptions, and code correctness still requires our own testing.

## Decision

1. Separate `ObservedMeasurement`, `ModelPrediction` and `BusinessRecommendation`, each with a provenance link to local run artifacts, scenario and environment fingerprints.
2. Capacity findings need independent calibrated load generation, realistic active-user mix, steady-state and repeat trials, predeclared SLO, and uncertainty. Unknown or incompatible observations are `INCONCLUSIVE`, never converted to PASS.
3. Only qualified measurements may be used to say the tested business target was met. Out-of-range USL extrapolation is a **scenario forecast**, not a verified server limit.
4. Identifying a bottleneck or recommending a resource upgrade requires permissioned observability and/or controlled comparisons. Correlation alone is a hypothesis.
5. Constrain default execution to authorized targets with bounded load; keep real target addresses, scenarios and report contents private to the operator.
6. K6 is a development-only benchmark reference, not a runtime engine or source of reusable AGPL-licensed implementation.

## Consequences

- Implement the engineering/validation stages in [RESEARCH_TO_CAPACITY_PLAN.md](../RESEARCH_TO_CAPACITY_PLAN.md) before release-level “sustainable capacity” marketing.
- UI can still show `PASS` at a tested level, but the evidence quality must be labeled.
- Reports distinguish `MEETS_TARGET`, `BELOW_TARGET`, `TEST_RANGE_TOO_LOW`, `UNKNOWN_OR_INCOMPARABLE`, and `CHANGE_RECOMMENDED_FOR_EVALUATION`.
- Feature releases must include an evidence-level statement, explicit non-goals, privacy checks and deterministic regression tests.

## Source of decision

[Little (1961)](https://doi.org/10.1287/opre.9.3.383); [Schroeder et al. (2006)](https://www.usenix.org/conference/nsdi-06/open-versus-closed-cautionary-tale); [Almeida & Menascé (2002)](https://doi.org/10.1109/MITP.2002.1046642); [Gunther (2008)](https://arxiv.org/abs/0808.1431); [Georges et al. (2007)](https://biblio.ugent.be/publication/417084).
