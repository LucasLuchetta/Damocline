# ADR 0098: Bound expanded request templates

## Status

Accepted.

## Context

Extracted response values are bounded, and configured bodies/headers are bounded before interpolation. Repeating a captured value many times in a template can still create an oversized outgoing request. That work consumes generator memory and can prevent the configured workload from reaching its target.

## Decision

Limit base origins to 4 KiB and configured path/query templates to 16 KiB before per-request parsing. Expand path segments and encoded query values incrementally within a 16 KiB intermediate budget, and stop as soon as the complete URL would exceed 16 KiB, an expanded header value would exceed 8 KiB, total configured header bytes (name, value, and framing allowance) would exceed 64 KiB, or the expanded request body would exceed 64 KiB. Reject over-limit output before `http.Client.Do`. Apply the same local rejection classification to missing variables and request-build errors. Persist only a request-construction rejection count and a generic failure reason in report schema v29.

Local request-construction rejections count as failed planned request observations for accounting, but they do not count as HTTP attempts or target responses. Measurement quality becomes INVALID and campaign assessment returns `INVALID_DELIVERY`, so a local generation failure cannot be attributed to the target. Redacted exports may include the safe count but never the expanded content.

## Consequences

Some previously accepted large dynamic templates will now fail locally. Each request's template expansion and retained request fields have explicit caps. The limits do not bound aggregate memory across all configured workers or application response parsing beyond existing response limits.

## Validation

Bounded loopback tests extract an 8 KiB synthetic value, repeat it in a request body and a header, and verify those oversized requests are rejected before the target observes them. Report tests and campaign classification preserve the local rejection as invalid delivery evidence.
