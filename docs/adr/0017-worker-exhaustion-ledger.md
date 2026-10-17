# ADR 0017 — Reconcile worker exhaustion with the reference server

## Status

Accepted for a bounded validation slice; G0-04 and G0-15 remain open.

## Context

Arrival-mode busy drops are created by the local scheduler when no synthetic worker is ready. Existing tests checked that drops were reported, but did not reconcile those client-side drops with an independent server ledger in the same run.

## Decision

Add a native-engine integration test with one worker, four planned journey starts per second, and a bounded loopback response delay that keeps the worker occupied for the remaining arrival slots. Assert all four starts are accounted for, one is admitted, three are classified as busy drops, and the independent server sees exactly the one admitted request. Assert the run cannot pass when offered arrivals are dropped.

This test validates workload delivery accounting only. It does not infer application saturation from worker exhaustion.

## Safety and limitations

The test is limited to a one-second arrival window, one worker, four planned starts, and an ephemeral local fixture. No external targets or credentials are involved. OS-load effects, campaign stage transitions, and broader generator envelopes remain unvalidated; run quality remains limited without equivalent preflight evidence.
