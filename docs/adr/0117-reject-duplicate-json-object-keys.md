# ADR 0117: Reject duplicate JSON object keys in reproducible artifacts

## Status

Accepted

## Context

Go's standard JSON decoder accepts repeated object keys and keeps the later value. Other parsers may keep the first value or reject the document. Escaped keys can also collide with unescaped spellings after decoding, such as `schema_version` and `schema_\u0076ersion`. A saved scenario or evidence file can therefore have one byte digest but more than one plausible interpretation across tools, weakening workload fingerprints and offline replay.

## Decision

Before decoding bounded scenario, campaign profile, campaign, run, calibration, acceptance, history, dashboard, goal, and export artifacts, scan the complete JSON structure and reject any object that repeats a decoded key. Arrays and nested objects are traversed recursively; identical names in separate objects are allowed. Existing per-artifact size limits and schema validation remain in force.

## Consequences

Previously accepted ambiguous files now fail with a clear input error. Valid historical files with unique keys continue through their existing schema gates and migration rules. This does not authenticate report authors or make evidence tamper-proof; exact byte hashes remain identifiers of the input bytes.

## Validation

Unit tests cover duplicate keys at the top level, inside nested objects and arrays, and keys that collide after Unicode escape decoding. Reader tests cover scenarios, profiles, campaigns, run reports, calibration, acceptance, history, dashboard, goal, and export paths and verify rejected exports do not create output.
