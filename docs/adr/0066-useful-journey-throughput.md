# ADR 0066: Useful configured journey throughput

## Status

Accepted

## Context

Run output reported achieved HTTP requests per second and a configured request
rate ceiling. It did not provide a separate rate for successful completion of
the configured workflow. Request count can rise while useful workflow
completion falls, and cancelled workflows are not completed transactions.

## Decision

Calculate successful completed configured journeys per recorded observation
second. Report completed journey rate (including failures) and cancelled
journey counts separately. Show the derived rates in run and campaign output,
offline insights, and comparisons. Add min/max successful journey throughput
to campaign variability and bump the campaign result schema to v3. Continue to
read and assess schema-v1/v2 campaigns; absent journey-throughput spread in
older reports is unknown.

## Consequences

The output distinguishes the HTTP request governor, request throughput, and
successful workflow throughput. The rate describes the configured HTTP
workflow only; it does not measure browser rendering, unmodeled user actions,
or real human demand. Cancelled journeys remain incomplete, and neither a
passing rate nor a stable range certifies server capacity. The campaign range
is descriptive and not a confidence interval.
