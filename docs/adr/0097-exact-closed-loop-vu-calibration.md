# ADR 0097: Calibrate exact closed-loop virtual-user profiles

## Status

Accepted (experimental).

## Context

The schema-v8 calibration matrix measured only open-arrival workloads. Closed-loop campaigns use virtual users and cannot safely reuse arrival-rate measurements. The existing campaign gate therefore remained BLOCKED for every closed-loop profile.

## Decision

Advance preflight evidence to schema v9. Preserve the exact schema-v8 arrival matrix and append six bounded closed-loop HTTP profiles at 1, 2, 5, 10, 20, and 50 virtual users. Each profile uses GET `/work`, a 4 KiB response, a two-second observation, a one-second timeout, a recorded shared 100 requests/s ceiling, connection reuse, zero ramp-up, and zero think time.

Closed-loop evidence records mode explicitly and leaves arrival scheduling/admission/drop counters inapplicable and zero. Calibration and acceptance validate completed iterations against client body observations and independent loopback server request, byte, and TCP ledgers. They also bound completed iterations to the recorded shared `max_rps` token rate over the observation window, with a per-VU allowance for ticker delivery at the boundary. Closed-loop profile acceptance does not claim an exact TCP count because concurrent users may establish connections nondeterministically.

The closed-loop campaign gate requires an exact VU level and exact request ceiling, duration, timeout, connection policy, workload shape, and timing controls. It blocks interpolation and remains opt-in. The acceptance manifest advances to schema v5 and requires all 39 profiles.

## Consequences

The local generator can now be preflighted for the six exact short VU tuples. These observations do not establish target identity, physical host identity, longer campaign headroom, sustainable application capacity, or coverage for other timing and request controls. Historical schema-v1 through v8 calibration records retain their earlier validation semantics and cannot qualify closed-loop campaigns.
