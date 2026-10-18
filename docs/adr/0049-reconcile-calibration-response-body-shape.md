# ADR 0049: Reconcile calibration response-body shape

## Status

Accepted.

## Context

The local healthy-stage fixture declared a fixed 4 KiB response, and its independent server ledger reconciled request counts. However, the stage acceptance gate did not inspect the body's size actually observed by the native HTTP engine. A fixture implementation regression could therefore leave the calibration metadata describing 4 KiB while the client received another body size.

## Decision

Require every healthy calibration stage to contain exactly one successful `GET /work` step and reconcile its complete response-body observations against the aggregate observations. Both ledgers must report the planned number of successful requests and complete bodies, no failed/cancelled requests or incomplete/oversized reads, a 4 KiB minimum and maximum, and the corresponding total byte count. Keep this exact-size check limited to the existing fixture; do not infer coverage for other sizes.

## Consequences

The recorded 4 KiB calibration dimension is now based on native-engine observations as well as the fixture declaration and independent request counters. Missing or inconsistent step/body evidence fails the calibration with a specific diagnostic. Response sizes beyond this one fixture remain outside the calibrated workload envelope.
