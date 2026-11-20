# ADR 0143: require recorded phase evidence state

Closed-loop campaign artifacts with predeclared objectives (schema v13+)
must record `evidence_state` for every saved phase, including phases in a
pending trial. The shared raw reader enforces presence before typed decoding.
Assessment continues to recompute the classification and compare it with
the recorded value.

Earlier schemas retain their historical reader rules.
