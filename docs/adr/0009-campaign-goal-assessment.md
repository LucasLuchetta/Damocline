# ADR 0009: Business goals require complete repeated campaign evidence

## Status

Accepted.

## Decision

Provide an offline `plan` command that applies the existing peak-demand,
growth, and reserve calculation to a saved campaign report. The command
accepts a positive conclusion only when the report has the expected schema,
scenario, exact target virtual-user level, two to five complete trials, passing
observation plus recovery phases in every trial, and `VALID_WITHIN_ENVELOPE`
measurement quality for every required observation, spike, and recovery phase.
Missing, `LIMITED`, `INVALID`, or unknown quality remains
`INSUFFICIENT_EVIDENCE`.

The offline evaluator recomputes quality for each required phase from its saved
run fields and requires the recomputed state to match the persisted state. A
campaign cannot make itself eligible by changing only `data_quality.state` to
`VALID_WITHIN_ENVELOPE`. The current run assessor does not emit that state, so
recomputation remains `LIMITED` or `INVALID` until the generator-equivalence
evidence gate is implemented.

The resulting state is `GOAL_MET_UNDER_TESTED_CONDITIONS`. It means that the
recorded synthetic workload met the calculated demand under the saved
workflow, environment, SLOs, observation windows, generator evidence, and
variation after the positive generator-equivalence quality gate is available.
Current engine reports remain `LIMITED`, so they cannot produce this positive
state. It does not estimate a hardware limit, establish equivalence to
people, identify a bottleneck, or recommend an infrastructure change.

## Consequences

An incomplete, malformed, failed, or different-than-target campaign remains
`INSUFFICIENT_EVIDENCE`. The assessment reads bounded local JSON and generates
no network traffic. Users still need representative workload design,
generator calibration on the campaign host, longer observations where needed,
and diagnostic telemetry before making an investment decision.
