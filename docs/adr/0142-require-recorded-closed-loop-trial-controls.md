# ADR 0142: require recorded closed-loop trial controls

Closed-loop schema v9 introduced cooldown and trial IDs. Validate presence
of `cooldown` and every `run_id` in the shared artifact reader from v9
onward. Apply the same rule to pending trials in checkpoint artifacts.
This keeps dashboard, export and assessment aligned on field presence.

Keep the existing assessment checks for cooldown duration, ID format and
uniqueness. Earlier schemas retain their historical reader behavior.
