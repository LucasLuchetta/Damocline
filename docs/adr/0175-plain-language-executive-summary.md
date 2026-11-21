# ADR 0175: Plain-language executive summary in capacity reports

## Status

Accepted.

## Context

The executive HTML report (ADR 0173) still assumes the reader knows what
modeled active users, assessment states and p95 mean. Non-technical
decision makers need one sentence they can repeat, how much to trust it,
where the system stopped coping, and an explicit warning against reading
virtual users as people.

## Decision

Both the HTML (`--html-out`) and Markdown (`--report-out`) capacity reports
start with an executive summary derived only from saved evidence:

- a headline sentence. Only `GOAL_MET_UNDER_TESTED_CONDITIONS` produces
  "supports N concurrent active virtual users doing <journey> with request
  p95 under X ms and request errors under Y%"; other states say that no
  sustainable level is approved;
- a confidence statement: decision confidence, measurement quality and the
  repeat pass count with its exact interval and assumptions;
- the observed knee: the highest level where every measurement passed and
  the next level with an explicit FAIL, or a statement that the limit is
  outside the tested range or unknown. Levels without a recorded outcome
  are never counted as passes or failures;
- what saturated, always labeled as a correlational hypothesis: a throughput
  gain smaller than half the load gain between consecutive levels and the
  first breaching workflows. It never names a root cause;
- a "What this does NOT mean" box: virtual users are not registered users,
  accounts, sessions, requests in flight or requests per second; results
  are limited to the tested conditions; and the optional declared
  `--actions-per-user-minute` conversion to people is shown only as an
  estimate with its assumptions.

The HTML summary includes an inline SVG line chart of worst request p95
(left axis) and mean successful requests/s (right axis) per load level. It
uses no scripts or external assets. Technical sections remain below.

## Consequences

- Readers see the decision in plain words without losing the fact,
  hypothesis and unknown separation.
- The knee and saturation heuristics are descriptive. They can be
  inconclusive with few levels and do not replace diagnostic evidence.
- Golden tests with synthetic loopback fixtures cover passing, failing and
  insufficient outcomes.
