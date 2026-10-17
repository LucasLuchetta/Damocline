# ADR 0009: Business goals require complete repeated campaign evidence

## Status

Accepted.

## Decision

Provide an offline `plan` command that applies the existing peak-demand,
growth, and reserve calculation to a saved campaign report. The command
accepts a positive conclusion only when the report has the expected schema,
scenario, exact target virtual-user level, two to five complete trials, and passing
observation plus recovery phases in every trial.

The resulting state is `GOAL_MET_UNDER_TESTED_CONDITIONS`. It means that the
recorded synthetic workload met the calculated demand under the saved
workflow, environment, SLOs, observation windows, generator evidence, and
variation. It does not estimate a hardware limit, establish equivalence to
people, identify a bottleneck, or recommend an infrastructure change.

## Consequences

An incomplete, malformed, failed, or different-than-target campaign remains
`INSUFFICIENT_EVIDENCE`. The assessment reads bounded local JSON and generates
no network traffic. Users still need representative workload design,
generator calibration on the campaign host, longer observations where needed,
and diagnostic telemetry before making an investment decision.
