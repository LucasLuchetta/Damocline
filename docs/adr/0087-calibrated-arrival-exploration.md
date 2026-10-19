# ADR 0087: Restrict arrival exploration to exact calibrated rates

## Status

Accepted (experimental).

## Context

The open-arrival reference matrix qualifies only an explicit set of exact
rates, request shapes, connection policies, worker limits and control tuples.
The adaptive `explore-arrival` search can generate intermediate levels based on
observed outcomes. Checking only the requested endpoints would allow an
uncalibrated intermediate rate to run after traffic had already started.

## Decision

When `explore-arrival --calibration` is supplied, assess every integer rate in
the requested interval before traffic. Require an exact matching passing
profile for the requested minimum. Restrict the adaptive search to the sorted
set of compatible calibrated rates and lower the effective maximum to the
highest available profile. If an exact business-demand target lies inside the
requested interval, require an exact profile for that rate. `--require-calibration`
requires the calibration path; providing `--calibration` activates the gate
whether or not the require flag is set.

Persist calibrated explorations as schema v2 with requested bounds and the
qualified rate set. Preserve schema-v1 compatibility for uncalibrated reports.
The offline goal assessor validates the v2 envelope and rejects every probe
outside the persisted calibrated set. Underlying native runs retain calibration
coverage as descriptive provenance.

## Consequences

No rate is interpolated or run outside the exact qualified set. A successful
search to its effective ceiling makes no statement about a higher requested
rate that lacked a profile. The reference still qualifies only local generator
delivery for short synthetic profiles; it does not establish target identity,
response behavior, workload representativeness, or sustained generator
headroom. Closed-loop campaign calibration remains unsupported because the
current matrix measures open-arrival workloads.
