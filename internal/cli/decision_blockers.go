package cli

import (
	"fmt"
	"io"

	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
)

func writeDecisionBlockers(w io.Writer, demand *capacityplan.Result) {
	if demand == nil || len(demand.Blockers) == 0 {
		return
	}
	writeDecisionBlockerGroups(w, demand.Blockers)
}

func writeDecisionBlockerGroups(w io.Writer, blockers []capacityplan.DecisionBlocker) {
	groups := []struct{ key, label string }{
		{key: capacityplan.BlockerConfiguration, label: "Configuration and scenario"},
		{key: capacityplan.BlockerEvidence, label: "Insufficient evidence"},
		{key: capacityplan.BlockerDamocline, label: "Damocline limitations"},
	}
	for _, group := range groups {
		heading := false
		for _, item := range blockers {
			if item.Category != group.key {
				continue
			}
			if !heading {
				fmt.Fprintf(w, "\n### %s\n\n", group.label)
				heading = true
			}
			fmt.Fprintf(w, "- %s %s\n", markdownText(item.Finding), markdownText(item.Action))
		}
	}
}
