# ADR 0119: Keep capacity report content independent of output destination

## Status

Accepted

## Context

Campaign audit replay can reproduce the same assessment from the same source
bytes, auditor executable, and demand inputs. The generated Markdown also
included the local path chosen for the output file. That path changes when a
reviewer replays to a new destination, even though it is not evidence about the
campaign, and it makes otherwise identical reports differ.

## Decision

Capacity Markdown renderers do not include their output destination in report
content. The destination still controls where the file is written and retains
the existing no-overwrite behavior. The source campaign path remains in the
replay command because it identifies the input to reproduce.

## Consequences

- With the same source path and bytes, auditor executable, and demand inputs,
  repeated reports are byte-identical even when written to different output
  paths.
- Reports no longer disclose the local report destination, but may still
  contain private source paths, target origins, and scenario/profile labels.
- Reproduction requires the exact recorded auditor binary and unchanged source
  evidence; digests identify bytes but do not authenticate their producer.

## Validation

The end-to-end loopback campaign test writes the same assessment and Markdown
report to two different output paths, compares both artifacts byte for byte,
and confirms offline replay emits no target traffic. Golden report coverage
checks the path-free footer.
