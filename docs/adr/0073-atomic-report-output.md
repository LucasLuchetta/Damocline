# ADR 0073: Write report artifacts atomically

## Status

Accepted

## Context

Commands write bounded JSON evidence after running a workload. Writing directly
to the final path can truncate an earlier report before the new JSON has been
fully written. A serialization, disk, process, or filesystem failure can leave
the destination incomplete.

## Decision

Serialize and validate the complete output size before touching the destination.
Write the bytes to a mode-restricted temporary file in the destination
directory, synchronize and close it, then replace the destination. On Unix,
synchronize the containing directory after rename. On Windows, use
`MoveFileExW` with replace-existing and write-through flags. Remove the
temporary file when any pre-replacement step fails.

## Consequences

An existing report remains intact until a complete replacement is ready. A
failure to sync the Unix directory happens after rename; the new report is
already visible, but the returned error makes uncertain crash durability
explicit. Unix reports use mode `0600`; Windows access is governed by the
destination directory's inherited ACL, since Go permission bits do not
configure Windows ACL entries. The output size cap includes the final newline.
This does not provide versioned report history, retention or recovery from
storage hardware that violates sync semantics. The local archive uses immutable
timestamp/content-digest IDs and explicit list, prune and delete commands. It
stops at a fixed report-count/byte limit rather than automatically discarding
evidence. Archiving an arbitrary JSON file does not sanitize its contents; only
intended local Damocline artifacts should be archived.
