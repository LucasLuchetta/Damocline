# ADR 0027: Report generator sampler cadence and collection time

## Status

Accepted.

## Context

Generator-health reports included sample counts and peaks but did not show whether samples were far apart or how long collection calls took. A low count alone cannot distinguish a short workload from a delayed sampler.

## Decision

Schema-v10 run summaries record the requested sampler interval, maximum monotonic sample-to-sample gap, and total/maximum wall time spent collecting each runtime and supported process snapshot. Quality validation treats malformed counters/timings as INVALID and a gap longer than two configured intervals as LIMITED. Mean process CPU is normalized by recorded GOMAXPROCS and includes OS process CPU time. A window-mean ratio at or above 90% marks sustained generator headroom LIMITED. The maximum adjacent 250 ms interval ratio remains diagnostic; it can exceed 100% because OS process CPU includes system activity that is not bounded by Go P occupancy, and does not alone establish sustained saturation. Collection wall time is reported as a proxy; it is not relabeled as CPU overhead.

## Consequences

Reports make sampler coverage and collection cost visible and block a quality upgrade when the sampler is stale or contradictory. This is partial G0-07 evidence: direct CPU-overhead calibration, RSS accuracy under host load, and cross-platform repeatability remain open. Older reports remain readable with absent sampler fields treated as unknown.
