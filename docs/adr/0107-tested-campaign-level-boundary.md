# ADR 0107: Report tested campaign level boundaries explicitly

## Status

Accepted

## Context

Campaign level collections retained every level and classified pass, fail, and inconclusive outcomes. Reviewers still had to infer the highest passing level and first failing level from lists and patterns. A collection with only passing results could also be misread as an established capacity ceiling.

## Decision

Persist the highest passing tested level, the first failing tested level, and an explicit upper-bound state in the collection assessment. Mark the points as a tested bracket only when the observed pattern is monotonic pass-below-fail. All-pass observations retain an unknown upper bound; non-monotonic observations are explicitly unknown. Inconclusive, invalid, and non-comparable collections also retain an unknown bound.

## Consequences

JSON and Markdown reports make the tested boundary easier to review and reproduce while retaining all per-level evidence. The fields describe observed points only. They do not establish results at untested levels, a monotonic capacity curve, statistical confidence, or a universal application capacity.
