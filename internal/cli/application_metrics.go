package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/appmetrics"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

type phaseRunner func(context.Context, config.Validated) report.Result

func validateApplicationMetrics(endpoint string, interval time.Duration) error {
	if endpoint == "" {
		return nil
	}
	if _, err := appmetrics.ValidateEndpoint(endpoint); err != nil {
		return err
	}
	return appmetrics.ValidateInterval(interval)
}

func withApplicationMetrics(endpoint string, interval time.Duration, budget *appmetrics.Budget, runner phaseRunner) phaseRunner {
	if endpoint == "" {
		return runner
	}
	return func(ctx context.Context, scenario config.Validated) report.Result {
		result, timeline := appmetrics.CaptureDuring(ctx, endpoint, interval, budget, func() report.Result {
			return runner(ctx, scenario)
		})
		result.ApplicationMetrics = &timeline
		return result
	}
}

func printApplicationMetricsSummary(w io.Writer, timeline *appmetrics.Timeline) {
	if timeline == nil {
		return
	}
	fmt.Fprintf(w, "Application metrics: %s | %d successful snapshots, %d failed, %d interrupted | interval %s.\n",
		timeline.State(), len(timeline.Samples), timeline.FailedCollections, timeline.InterruptedScrapes, timeline.Interval)
	fmt.Fprintln(w, timeline.Note)
}
