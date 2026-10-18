# ADR 0016 — Reconcile interrupted arrivals with an independent server ledger

## Status

Accepted for a bounded validation slice; G0-04 and G0-15 remain open.

## Context

Arrival scheduling already accounts for planned starts, admitted starts, worker drops, and late drops. The engine also records cancelled requests and journeys. Existing loopback matrix checks reconcile completed traffic, while cancellation checks did not jointly compare the interrupted client report with an independent server ledger.

## Decision

Add an integration test using a bounded, delayed loopback reference handler. The test waits until one request is independently observed, cancels the run, and verifies these separate conservation relationships:

- planned arrivals equal admitted starts plus dropped arrivals;
- dropped arrivals equal busy-worker plus late-scheduler drops;
- started journeys equal completed plus incomplete journeys;
- server-received requests equal client-recorded completed plus cancelled requests;
- server-received requests equal server-completed plus server-cancelled plus server-rejected requests.

The test covers cancellation only. It does not alter engine runtime behavior, safety ceilings, or positive data-quality certification.

## Safety and limitations

Traffic is limited to one request against an ephemeral loopback test server with a two-second fixture delay and a three-second configured run bound. No external target or credentials are used. This evidence does not cover OS-load effects, worker exhaustion, every campaign boundary, or a workload-specific generator envelope; quality remains limited without equivalent preflight evidence.
