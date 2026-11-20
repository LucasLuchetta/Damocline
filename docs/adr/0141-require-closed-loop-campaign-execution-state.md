# ADR 0141: require recorded closed-loop campaign execution state

Closed-loop campaign schema v10 introduced execution checkpoints. Require
the recorded `execution_state` field from that version onward in the
shared raw artifact reader. This gives dashboard, export and assessment
the same missing-field check before JSON decoding.

Assessment retains the existing semantic checks for the execution state
and its relationship to a pending trial. Schemas before v10 retain their
historical reader behavior.
