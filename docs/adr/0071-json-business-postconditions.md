# ADR 0071: JSON business postconditions

## Status

Accepted (experimental)

## Context

HTTP success and expected status alone do not establish that a business action completed. Text matching helps for simple responses, but structured APIs need a bounded way to check a state field without retaining response bodies in reports.

## Decision

Allow each step to declare up to 20 JSON Pointer to scalar string-value assertions. After response status and optional text checks, parse the bounded response body and compare each selected scalar's string representation to its configured expected value. Malformed JSON, a missing pointer, a nonscalar value, or a mismatch fails the step with a generic diagnostic. The assertions enter the workload fingerprint; their values and response content are not persisted in run reports.

## Consequences

The engine can distinguish a false HTTP-200 business result from a completed step. This is not a general JSONPath language, stateful post-run verification, or proof that remote side effects are durable. Expected values are scenario configuration and should be synthetic or otherwise safe to store locally.
