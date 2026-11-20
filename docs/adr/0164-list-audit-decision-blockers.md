# ADR 0164: List actionable audit decision blockers

Keep the existing thresholds and conservative decision states. For readable
campaign artifacts, collect independent decision failures instead of returning
after the first failed gate. Group distinct findings under configuration and
scenario, insufficient evidence, and Damocline limitations. Pair each finding
with an action. Retain the existing findings field and add structured blockers
without changing the interpretation of earlier artifacts.

An unreadable artifact must first pass the bounded reader. Missing counters
cannot support statistical calculation. Repeated failures of the same gate
are listed once per series type. The phase and window ledgers retain the
individual observations.
