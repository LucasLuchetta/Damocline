package cli

import (
	"context"

	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/report"
)

func rotateCampaignCookies(next phaseRunner, cookies [][]string) (phaseRunner, func(int)) {
	trial := 1
	selectTrial := func(number int) { trial = number }
	if len(cookies) == 0 {
		return next, selectTrial
	}
	return func(ctx context.Context, v config.Validated) report.Result {
		v.Cookies = cookies[trial-1][:v.Scenario.Users]
		return next(ctx, v)
	}, selectTrial
}
