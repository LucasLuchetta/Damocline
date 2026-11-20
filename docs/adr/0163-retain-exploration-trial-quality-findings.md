# ADR 0163: Retain exploration trial quality findings

Closed-loop exploration already stores complete run reports in
`probes[].runs[]`. The console condensed them to latency, errors and
volume, and the explorer could discard accounting findings that it
recomputed itself.

Store recomputed invalid findings in the existing `data_quality` field
and display invalid findings under the corresponding VU level and
trial. Retain the existing bounded JSON writer and schema. This makes
the original diagnosis available without replaying traffic.
