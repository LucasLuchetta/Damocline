# ADR 0094: Rotate configured authentication cookie sets between campaign trials

## Status

Accepted (experimental).

## Context

Closed-loop and open-arrival campaigns already group phases into independent trials with cooldowns and random run IDs. Environment-backed per-actor or per-workflow cookies were reused for every repeat, so sequential trial effects could be confused with repeated use of one authenticated session.

## Decision

Allow one `{trial}` token in `per_vu_cookie_env_prefix` and expand it to the one-based trial number. If any configured cookie prefix is trial-scoped, require every configured prefix to use the token. Resolve every actor and workflow for every repeat before starting any phase; closed-loop campaigns account for the maximum base or spike actor count. Preserve each trial's credentials across its phases. Reject missing credentials and duplicate canonical cookie sets across actors, workflows, or trials. Keep credential values and environment-variable names in memory only.

Advance run reports to schema v29 and persist only the fixed `authentication_session_scope: trial_scoped_cookie_sets` enum for these phases. Redacted exports preserve only that enum. Do not add credential material or prefix names to reports, fingerprints, or output.

## Consequences

Campaigns can use separately configured cookie sets across trials and fail closed before traffic when a repeat is incomplete. Unconfigured authentication contexts remain empty jars. Distinct canonical cookie sets do not prove distinct accounts, authorization, validity, freshness, or data isolation. There is no automatic login, refresh, or application-specific data reset.

## Validation

Deterministic configuration tests exercise prefix expansion and duplicate sets. Custom campaign runners verify credentials remain stable within a trial and change between trials in both workload modes; missing later-trial credentials fail before runner invocation. Tests use synthetic environment values and loopback-only fixtures.