// damocline-acceptance runs the bounded local G0 evidence lab.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/LucasLuchetta/Damocline/internal/acceptancelab"
)

func main() {
	calibrator := flag.String("calibrator", "", "path to the locally built damocline-calibrate executable")
	output := flag.String("out-dir", "", "new directory for this run's evidence (must not already exist)")
	repeats := flag.Int("repeats", 2, "independent calibration process runs (2..3)")
	extended := flag.Bool("extended", false, "include the bounded 15/20/50/100 arrivals/s stages; permits at most two repeats")
	timeout := flag.Duration("run-timeout", acceptancelab.DefaultRunTimeout, "per-process deadline, from 30s through 4m")
	flag.Parse()
	if *calibrator == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: damocline-acceptance --calibrator <path> --out-dir <new-private-directory> [--repeats 2] [--extended]")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := acceptancelab.Run(ctx, acceptancelab.Options{
		CalibratorPath: filepath.Clean(*calibrator), OutputDir: filepath.Clean(*output),
		Repeats: *repeats, Extended: *extended, RunTimeout: *timeout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if result.Status != "" {
			fmt.Fprintf(os.Stderr, "Evidence: %s\n", filepath.Join(*output, "manifest.json"))
		}
		os.Exit(1)
	}
	fmt.Printf("%s: %d independent calibration runs and %d local HTTP transport cases. Evidence: %s\n",
		result.Status, len(result.Attempts), result.Configuration.TransportCases, filepath.Join(*output, "manifest.json"))
}
