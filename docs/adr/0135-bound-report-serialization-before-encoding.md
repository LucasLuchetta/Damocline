# ADR 0135: bound report serialization before encoding

## Status

Accepted.

## Context

The exact 16 MiB output limit was checked after `json.MarshalIndent` allocated
complete encodings. Rejecting an oversized artifact therefore did not bound
serialization memory. A Windows regression with a preallocated 64 MiB string
observed approximately 320 MiB of additional allocations without preflight.

## Decision

Keep the exact serialized output limit, newline, standard-library encoding and
atomic file replacement. Inspect the artifact model before encoding with a
32 MiB accounting budget for escaped strings, numeric representation, structural
work, binary expansion, reflection copies, field metadata and indentation. Charge map key/value
copy sizes before MapIter can allocate large inline values. Bound inspection
nesting at 128 levels.
The accounting budget is a resource guard, not a process-memory ceiling or a
replacement for the exact output-size check. It can conservatively reject a
model with large hidden/promoted fields even if JSON field selection would omit
them. Existing exact-limit output fixtures must continue to pass.

Accept plain artifact values and the bounded standard-library `time.Time`,
`json.Number` and `json.RawMessage` representations. Raw fragments have a 16 MiB
input cap and are checked for JSON validity, nesting, HTML escaping and pretty
printing cost. Inspect exported children of unexported embedded structs too. Charge potential
encoded names even for omitted fields, before the standard encoder can cache
them; charge tag lookup work and the larger of tag/fallback field names.
Inspect reachable type metadata once even behind nil/omitted values, because
the encoder constructs those descendants before considering values. Precharge
the possible byte/node work of builtin `IsZero`, including hidden fields and
zero-sized array elements, before testing composite `omitzero` fields.
Reject arbitrary JSON/text serialization callbacks and custom `IsZero` methods
used by `omitzero`, including pointer receiver methods. Their allocation and
execution cannot be bounded by inspecting the underlying value. Ignored fields
and empty omitted fields do not execute callbacks.

Check already encoded artifact byte limits before JSON validation. A rejected
model must not create directories, temporary files, or replace an existing
report. Keep filesystem errors and atomic-write cleanup behavior intact.

## Validation and limitations

Regression tests compare supported values byte-for-byte with standard-library
encoding, preserve the exact 16 MiB output boundary, reject cycles/deep raw
fragments, and prove serialization callbacks are not executed. A compile overlay
disabling only preflight reproduces excessive allocation; the corrected test
requires less than 4 MiB of additional allocation for the rejected 64 MiB string.
A separate regression reproduced 67,109,600 allocation bytes from copying an
oversized inline map value during inspection; precharging the copy prevents it.
Two bounded fuzz targets compare string escaping and raw-fragment indentation
accounting with `encoding/json`.

A separate-process resource fixture performs 32 accepted 1 MiB writes and 32
rejected 64 MiB writes, reconciles saved bytes and temporary-file cleanup, checks
retained Go heap after two collections, and reads an OS process peak-RSS counter
against a predeclared 256 MiB fixture bound. Input construction is outside the
writer allocation assertion but remains included in child peak RSS. These are
bounded fixture observations, not universal memory, leak-freedom, full-disk,
crash-durability or filesystem-authenticity guarantees. Callers must not mutate
artifact values concurrently with inspection or encoding.

## Peak-memory measurement boundary

Windows reads `PeakWorkingSetSize`; Linux reads the current executable image's
`VmHWM` from a bounded `/proc/self/status` reader. The observation records an
explicit `peak_rss_method`. No counter is reset before measuring the fixture.
Linux `getrusage` metrics [persist across execve](https://www.man7.org/linux/man-pages/man2/getrusage.2.html),
so they can include the previous address space inherited from a larger test
parent. The [kernel proc documentation](https://www.kernel.org/doc/html/v6.17/filesystems/proc.html)
defines VmHWM as the address-space RSS high-water mark. These kernel reports are
operational observations, not a precise physical-memory certificate.

The full Linux package run initially failed the 256 MiB fixture ceiling with a
278,134,784-byte process-lifetime peak. A bounded launch diagnostic reproduced
199,424 KiB of lifetime history at child startup while that fresh image's VmHWM
was 2,176 KiB. A permanent integration regression keeps 128 MiB resident in the
parent, allocates 16 MiB in the child, and requires the child image peak to stay
below its separate 64 MiB bound. The report fixture retains its original 256 MiB
bound. The failed attempt remains local evidence; it is not relabeled as passing.
