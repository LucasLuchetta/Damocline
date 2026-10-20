# ADR 0103: Replayable capacity assessment reports

## Status

Accepted

## Context

The offline campaign audit already recomputes measurement and decision evidence and can emit machine-readable JSON. The JSON assessment retained the calculated demand level but omitted the peak demand, growth, and reserve inputs that produced it. The human CLI output mixed executive conclusions with technical diagnostics, which made it harder to review the verdict and the evidence supporting it as one reproducible artifact.

## Decision

Persist the original demand inputs in assessment schema v3 and campaign-collection schema v2. Add `audit --report-out FILE.md`, which writes executive and technical sections from the same assessment object used by JSON output. The technical section records the exact demand inputs, campaign schedule, phase workload controls, evidence digests, observation summaries, confidence limits, source path, and a PowerShell command that replays the offline assessment. Markdown output is bounded, created exclusively with mode 0600 where supported, synced, and never overwrites an existing file. On Windows, ACLs are inherited from the destination directory.

The report links the workload definition by its recorded fingerprint and explains how to run `campaign profile verify` against the saved private profile. It does not embed credentials or the complete scenario. The source campaign and its matching private profile remain separate artifacts.

## Consequences

The demand calculation can be independently recomputed from the persisted inputs. Re-running the command against the preserved source campaign must produce the same assessment verdict; the embedded evidence ID lets reviewers detect a different source artifact. Human and machine views derive from the same assessment, preventing separate decision logic from producing conflicting verdicts.

The Markdown report may include a target origin and private scenario labels. It must be kept with local evidence and not committed by default. Content digests are identifiers, not signatures; they do not authenticate the source, environment, host, target, or operator. A full workload rerun still requires the private scenario/profile and external credential/data setup.
