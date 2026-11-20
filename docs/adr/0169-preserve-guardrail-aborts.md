# ADR 0169: Preserve guardrail aborts alongside invalid delivery

When a generator guardrail stops a closed-loop campaign phase, early
cancellation can leave the phase with invalid delivery evidence. Preserve that
phase classification and the native diagnostics. The campaign and trial record
an abort with the existing GENERATOR_LIMITED reason so the audit can identify
the cause and exclude the phase from application capacity evidence.

The existing artifact fields carry both observations. Historical campaigns
retain their recorded state and require no schema migration.
