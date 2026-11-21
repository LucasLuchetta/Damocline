// Command scenario-to-k6 prints an equivalent closed-loop k6 script for a
// single-workflow Damocline scenario. Run it from the repository root:
//
//	go run ./lab/k6-compare/scenario-to-k6 scenario.json > compare.k6.js
package main

import (
	"fmt"
	"os"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/externalcompare"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: scenario-to-k6 scenario.json > script.js")
		os.Exit(2)
	}
	s, err := config.Load(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	script, err := externalcompare.K6Script(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(script)
}
