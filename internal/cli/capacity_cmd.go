package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
	"github.com/LucasLuchetta/Damocline/internal/config"
	"github.com/LucasLuchetta/Damocline/internal/engine"
	"github.com/LucasLuchetta/Damocline/internal/explorer"
	"github.com/LucasLuchetta/Damocline/internal/report"
	"github.com/LucasLuchetta/Damocline/internal/selfcalibration"
	"github.com/LucasLuchetta/Damocline/internal/statistics"
)

const capacityArtifactLimit = 16 << 20

func readCapacityFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, capacityArtifactLimit+1))
	if len(data) > capacityArtifactLimit {
		return nil, errors.New("capacity input exceeds 16 MiB")
	}
	return data, err
}

func readCapacityExplore(path string) (explorer.Result, error) {
	result, _, err := readCapacityExploreWithSHA256(path)
	return result, err
}

func readCapacityExploreWithSHA256(path string) (explorer.Result, string, error) {
	var result explorer.Result
	data, err := readCapacityFile(path)
	if err == nil {
		err = explorer.ValidateArtifactJSON(data)
	}
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	if err != nil {
		return explorer.Result{}, "", err
	}
	sum := sha256.Sum256(data)
	return result, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func capacityObservation(
	w io.Writer, result explorer.Result, v config.Validated, maxUsers int,
) (int, time.Duration, error) {
	required, err := statistics.ZeroFailureSamples95(v.Scenario.MaxErrorRate)
	if err != nil {
		return 0, 0, err
	}
	p95Samples, err := statistics.MinimumSamplesForPercentileRankInterval95(.95)
	if err != nil {
		return 0, 0, err
	}
	required = max(required, p95Samples)
	fmt.Fprintf(w, "Minimum samples per series and window: %d (conditional zero-error and p95-rank bounds).\n", required)
	users := capacityPassingLevel(result, v.Envelope.Mode == config.EnvelopeExtended)
	if users < 1 {
		return 0, 0, errors.New("explore recorded no reproducible passing capacity bracket")
	}
	if note := capacityUnresolvedNote(result); note != "" {
		fmt.Fprintln(w, note)
	}
	window, err := capacityObservationAt(w, result, v, maxUsers, users, required)
	return users, window, err
}

// capacityObservationAt sizes the observation window for one repeatedly
// passing explore level from that level's own measured delivery rates.
func capacityObservationAt(
	w io.Writer, result explorer.Result, v config.Validated, maxUsers, users, required int,
) (time.Duration, error) {
	if users > maxUsers || result.Repeats < 2 || result.Repeats > 4 {
		return 0, errors.New("explore evidence is outside the declared capacity controls")
	}
	slowest := math.Inf(1)
	for _, probe := range result.Probes {
		if probe.Users != users || probe.Status != "PASS" || len(probe.Runs) != result.Repeats {
			continue
		}
		for _, run := range probe.Runs {
			fingerprint := v.WorkloadFingerprint
			if v.Envelope.Mode == config.EnvelopeExtended {
				fingerprint = extendedLevelFingerprint(v, users)
			}
			if run.Target != v.Base.Scheme+"://"+v.Base.Host || run.Config.WorkloadFingerprint != fingerprint ||
				run.Config.Users != users || run.Config.P95LimitMS != v.Scenario.P95LimitMS ||
				run.Config.JourneyP95LimitMS != v.Scenario.JourneyP95LimitMS || run.Config.MaxErrorRate != v.Scenario.MaxErrorRate ||
				run.Outcome != "PASS" || run.ElapsedSeconds <= 0 ||
				len(run.Steps) != len(v.Steps) || len(run.Profiles) != len(v.Scenario.Profiles) {
				return 0, errors.New("explore trial does not match the scenario, target, or passing level")
			}
			consider := func(label string, count int) {
				rate := float64(count) / run.ElapsedSeconds
				slowest = min(slowest, rate)
				fmt.Fprintf(w, "  %s: %d / %.3fs = %.3f/s\n", label, count, run.ElapsedSeconds, rate)
			}
			steps := make(map[string]int, len(run.Steps))
			for _, step := range run.Steps {
				steps[step.Name] = step.Metrics.Total
			}
			for _, step := range v.Steps {
				count, ok := steps[step.Step.Name]
				if !ok {
					return 0, errors.New("explore trial is missing a configured step")
				}
				consider(step.Step.Name, count)
			}
			consider("completed journeys", run.Journeys.Total)
			for _, profile := range run.Profiles {
				consider(profile.Name+" journeys", profile.Journeys.Total)
			}
		}
		break
	}
	seconds := math.Ceil(float64(required) / slowest)
	if math.IsInf(slowest, 0) || secsInvalid(seconds) {
		return 0, errors.New("missing, zero, or invalid measured delivery rate for a required series")
	}
	fmt.Fprintf(w, "Slowest measured rate %.3f/s. Minimum window %.0fs; three-window observation %.0fs. Delivery may vary.\n", slowest, seconds, 3*seconds)
	if seconds > 60 {
		return 0, errors.New("required window exceeds 60s; simplify the workload or validate more VUs within the supported ceiling")
	}
	return time.Duration(seconds) * time.Second, nil
}

func capacityPassingLevel(result explorer.Result, extended bool) int {
	if result.HighestPassingUsers != nil && (result.Outcome == "BRACKETED" || result.Outcome == "PASSED_TO_CONFIGURED_CEILING" || result.Outcome == "ABORTED" || result.Outcome == "INCONCLUSIVE") {
		return *result.HighestPassingUsers
	}
	if extended && result.Outcome == "INCONCLUSIVE" && strings.HasPrefix(result.Reason, explorer.UncalibratedRefinementReason) {
		users := 0
		for _, probe := range result.Probes {
			if probe.Status == "PASS" && len(probe.Runs) == result.Repeats {
				users = max(users, probe.Users)
			}
		}
		return users
	}
	return 0
}

// capacityUnresolvedNote explains, for an exploration that stopped ABORTED or
// INCONCLUSIVE after a repeatedly passing level, which levels stayed
// unresolved and why. It is empty for a resolved exploration.
func capacityUnresolvedNote(result explorer.Result) string {
	if result.HighestPassingUsers == nil || (result.Outcome != "ABORTED" && result.Outcome != "INCONCLUSIVE") {
		return ""
	}
	return fmt.Sprintf("Explore stopped early (%s); continuing with the highest repeatedly passing level %d VUs. Levels above it are unresolved and were not concluded as passing or failing. Reason: %s", result.Outcome, *result.HighestPassingUsers, strings.TrimSpace(result.Reason))
}

func secsInvalid(seconds float64) bool {
	return math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 1
}

type capacityManifest struct {
	ScenarioSHA   string
	MaxUsers      int
	Budget        time.Duration
	ExploreBudget time.Duration
	LongRun       bool
	Calibration   string
	Mirror        capacityMirror
}

func capacityCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("capacity", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("config", "", "closed-loop scenario JSON")
	dir := fs.String("output-dir", "", "private directory for protocol artifacts")
	existing := fs.String("explore-file", "", "previous matching exploration used for offline dry-run")
	maxUsers := fs.Int("max-users", 20, "maximum VUs for exploration")
	minUsers := fs.Int("min-users", 2, "first VU level of the exploration ladder (use a higher start when lower levels are already known to pass)")
	exploreBudget := fs.Duration("explore-budget", 8*time.Minute, "exploration wall cap")
	budget := fs.Duration("budget", 12*time.Minute, "campaign wall cap including drain and network preflights")
	calibration := fs.String("calibration", "", "existing calibration artifact")
	long := fs.Bool("allow-long-run", false, "allow a larger bounded staging budget")
	staging := fs.Bool("staging-confirmed", false, "confirm this is staging")
	dry := fs.Bool("dry-run", false, "plan without traffic or hook execution")
	authorized := fs.Bool("authorized", false, "confirm authorization to benchmark")
	insecureHTTP := fs.Bool("allow-insecure-http", false, "allow plaintext HTTP outside loopback for every protocol stage")
	resume := fs.Bool("resume", false, "reuse completed stages from the private directory")
	intensity := fs.Float64("actions-per-user-minute", 0, "declared actions per person per minute")
	accessLog := fs.String("access-log", "", "local pseudonymous JSON-lines action log")
	hook := fs.String("session-hook", "", "explicit executable returning a JSON array of cookies")
	var hookArgs stringList
	fs.Var(&hookArgs, "session-hook-arg", "literal hook argument; repeat as needed")
	referenceReport := fs.String("reference-report", "", "recorded run report of the target used to size the self-calibration mirror")
	responseBytes := fs.Int("response-bytes", 0, "declared self-calibration mirror response size when no reference report is given")
	serverDelay := fs.Duration("server-delay", 0, "declared self-calibration mirror server delay when no reference report is given")
	calLevel := fs.Duration("calibration-level-duration", selfcalibration.DefaultLevelDuration, "duration of each self-calibrated level")
	extended := fs.Bool("extended-envelope", false, "use the calibration-gated extended envelope across capacity stages")
	extendedAck := fs.Bool("extended-load-authorized", false, "confirm the target owner authorized load above the standard ceilings")
	targetClass := fs.String("target-class", "", "target class for the extended envelope: lab or staging")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "capacity: %v\n", err)
		return 2
	}
	if *file == "" || (!*dry && (*dir == "" || !*authorized)) ||
		(*existing != "" && !*dry) || *long != *staging ||
		math.IsNaN(*intensity) || math.IsInf(*intensity, 0) || *intensity < 0 {
		return fail(errors.New("require --config and, for execution, --authorized and --output-dir; --explore-file is for dry-run; long runs require both staging flags"))
	}
	mirror := capacityMirror{FromReport: *referenceReport, ResponseBytes: *responseBytes, ServerDelay: *serverDelay, LevelDuration: *calLevel, Extended: *extended, TargetClass: *targetClass}
	if *referenceReport != "" && (*responseBytes != 0 || *serverDelay != 0) {
		return fail(errors.New("use either --reference-report or --response-bytes/--server-delay to size the calibration mirror"))
	}
	if *extended && (!*extendedAck || (*targetClass != "lab" && *targetClass != "staging")) {
		return fail(errors.New("--extended-envelope requires --extended-load-authorized and --target-class lab or staging"))
	}
	if !*extended && (*extendedAck || *targetClass != "") {
		return fail(errors.New("extended-envelope options require --extended-envelope"))
	}
	var extendedRequest *config.ExtendedEnvelopeRequest
	var envelopeArgs []string
	if *extended {
		extendedRequest = &config.ExtendedEnvelopeRequest{Acknowledged: true, TargetClass: *targetClass}
		envelopeArgs = []string{"--extended-envelope", "--extended-load-authorized", "--target-class", *targetClass}
	}
	if *calibration != "" {
		if loaded, err := loadAnyCalibration(*calibration); err != nil || loaded.scenario == nil {
			return fail(errors.New("--calibration must be a workload self-calibration artifact from `damocline calibrate` covering the planned levels"))
		}
	}
	if !*dry && *calibration == "" && !mirror.declared() {
		return fail(errors.New("automatic self-calibration needs --reference-report (a recorded run of the target) or --response-bytes and --server-delay; or pass an existing --calibration"))
	}
	if *accessLog != "" {
		if *intensity != 0 {
			return fail(errors.New("provide either --access-log or --actions-per-user-minute"))
		}
		value, err := capacityLogIntensity(*accessLog)
		if err != nil {
			return fail(err)
		}
		*intensity = value
		fmt.Fprintf(stdout, "Local access-log intensity hypothesis: %.2f actions per observed identity per minute.\n", value)
	}
	scenario, digest, err := config.LoadWithSHA256(*file)
	if err != nil {
		return fail(err)
	}
	if scenario.Mode != "" && scenario.Mode != "closed" {
		return fail(errors.New("capacity requires a closed-loop scenario"))
	}
	if scenario.Users > *maxUsers {
		return fail(errors.New("scenario users exceeds --max-users; lower scenario users or select a supported ceiling"))
	}
	sampleFloor, err := statistics.ZeroFailureSamples95(scenario.MaxErrorRate)
	if err != nil {
		return fail(err)
	}
	if *minUsers < 1 || *minUsers > *maxUsers {
		return fail(errors.New("--min-users must be between 1 and --max-users"))
	}
	opt := explorer.Options{MinUsers: *minUsers, MaxUsers: *maxUsers, Repeats: 2, StageDuration: capacityTiming.ExploreStage, Budget: *exploreBudget}
	if stage, ok := capacityExploreStage(scenario, opt.MinUsers, *referenceReport); ok {
		opt.StageDuration = stage
	}
	campaignOptions := campaign.Options{
		Warmup: capacityTiming.Warmup, Recovery: capacityTiming.Recovery, Observation: 45 * time.Second,
		Repeats: 2, Budget: *budget, LongRun: *long, StagingConfirmed: *staging,
	}
	if *extended {
		opt.Extended, opt.TargetClass = true, *targetClass
		opt.UserCeiling = config.ExtendedEnvelopeHardLimits.MaxUsers
		campaignOptions.UserCeiling = config.ExtendedEnvelopeHardLimits.MaxUsers
		campaignOptions.Guardrails, err = config.ExtendedGuardrails(extendedRequest.Guardrails)
		if err != nil {
			return fail(err)
		}
	}
	if err := explorer.Validate(opt); err != nil {
		return fail(err)
	}
	if *exploreBudget <= 0 || *budget <= 0 {
		return fail(errors.New("exploration and campaign budgets must be positive"))
	}
	if err := campaign.Validate(campaignOptions); err != nil {
		return fail(err)
	}
	if *long && scenario.Environment != "staging" {
		return fail(errors.New("long runs require a scenario declaring the staging environment"))
	}
	stepCount := len(scenario.Steps)
	for _, profile := range scenario.Profiles {
		stepCount += len(profile.Steps)
	}
	for _, workflow := range scenario.Workflows {
		stepCount += len(workflow.Steps)
	}
	if float64(sampleFloor)*float64(max(1, stepCount)) > 60*float64(scenario.MaxRPS) {
		return fail(errors.New("even the configured request ceiling cannot supply the minimum samples in a 60s window; use fewer steps or raise the request ceiling within supported limits"))
	}
	ladder := explorer.PlannedLevels(opt)
	calibrationLevels := ladder
	if *extended {
		calibrationLevels = capacityCalibrationLevels(opt, *calLevel)
	}
	// Size the explore budget from the actual stages: every planned level and
	// every reserved refinement probe runs opt.Repeats stages, each with the
	// engine's own bounded overhead (ADR 0170). A default budget grows to fit
	// (within the 15m explorer cap); an explicit budget that is too small is
	// refused before any traffic instead of aborting mid-ladder.
	overhead, err := capacityStageOverhead(scenario, opt.MinUsers, extendedRequest, *insecureHTTP)
	if err != nil {
		return fail(err)
	}
	probes := capacityWorstCaseProbes(ladder, calibrationLevels, *extended)
	stages := probes * opt.Repeats
	exploreNeed := time.Duration(stages) * (opt.StageDuration + overhead)
	explicitExploreBudget := false
	explicitCampaignBudget := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "explore-budget" {
			explicitExploreBudget = true
		}
		if f.Name == "budget" {
			explicitCampaignBudget = true
		}
	})
	// The overhead above is each bound at its cap (for example the 30s
	// network-baseline budget and the full drain allowance); real stages
	// rarely approach it. Refuse only when even the expected plan (ladder
	// levels without refinement, overhead capped at capacityExpectedOverhead)
	// cannot fit. When only the worst case exceeds the cap, warn: the
	// explorer enforces its budget itself and an early stop continues from
	// the highest repeatedly passing level (ADR 0170).
	expectedOverhead := min(overhead, capacityExpectedOverhead)
	expectedNeed := time.Duration(len(ladder)*opt.Repeats) * (opt.StageDuration + expectedOverhead)
	const exploreCap = 15 * time.Minute
	if expectedNeed > exploreCap {
		return fail(fmt.Errorf("explore needs about %s even without bracket refinement (%d levels x %d repeats of %s plus about %s expected overhead), above the 15m explore cap; lower --max-users for a shorter ladder or shorten the stages", expectedNeed.Round(time.Second), len(ladder), opt.Repeats, opt.StageDuration, expectedOverhead))
	}
	if *exploreBudget < expectedNeed && explicitExploreBudget {
		return fail(fmt.Errorf("--explore-budget %s is too small for %d levels x %d repeats of %s plus about %s expected overhead; use at least %s", *exploreBudget, len(ladder), opt.Repeats, opt.StageDuration, expectedOverhead, expectedNeed.Round(time.Second)))
	}
	if !explicitExploreBudget && *exploreBudget < exploreNeed {
		*exploreBudget = min(exploreCap, exploreNeed.Round(time.Second))
		opt.Budget = *exploreBudget
	}
	if exploreNeed > *exploreBudget {
		fmt.Fprintf(stdout, "WARNING: the worst-case explore plan (%s: %d stages including bracket refinement, each bound at its cap) exceeds the %s explore budget. The expected plan needs about %s. If explore stops early, capacity continues from the highest repeatedly passing level and states that higher levels were not explored.\n", exploreNeed.Round(time.Second), stages, *exploreBudget, expectedNeed.Round(time.Second))
	}
	calWall := time.Duration(0)
	if *calibration == "" {
		ladderWall, err := capacityCalibrationWall(len(calibrationLevels), *calLevel)
		if err != nil {
			return fail(err)
		}
		campaignWall, _ := capacityCalibrationWall(1, *calLevel)
		calWall = ladderWall
		if !*extended {
			// One campaign-level calibration for the selected level and for
			// each bounded step-down level (ADR 0176).
			calWall += time.Duration(1+capacityMaxStepDowns) * campaignWall
		}
	}
	fmt.Fprintln(stdout, "Plan:")
	fmt.Fprintf(stdout, "  1. Network baseline check (%s cap).\n", config.NetworkBaselineWallBudget)
	if *calibration == "" {
		source := "declared mirror sizes"
		if *referenceReport != "" {
			source = "mirror sizes from the recorded reference run"
		}
		envelope := "standard ceilings"
		if *extended {
			envelope = "extended-envelope gate (target class " + *targetClass + ")"
		}
		if *extended {
			fmt.Fprintf(stdout, "  2. One calibration for ladder, refinement and campaign candidates %v (%s, loopback mirror only, %s; up to %s).\n", calibrationLevels, source, envelope, calWall)
		} else {
			fmt.Fprintf(stdout, "  2. damocline calibrate for levels %v, then for each campaign level (%s, loopback mirror only, %s; up to %s).\n", ladder, source, envelope, calWall)
		}
	} else {
		fmt.Fprintf(stdout, "  2. Existing calibration artifact %s (no new calibration).\n", *calibration)
	}
	fmt.Fprintf(stdout, "  3. Explore levels %v with --calibration and --require-calibration (%s cap).\n", ladder, *exploreBudget)
	fmt.Fprintf(stdout, "     Explore sizing: %d stages (worst case %d probes including bracket refinement, x %d repeats) x (%s stage + %s overhead: network baseline cap, bounded drain, setup) = %s.\n",
		stages, probes, opt.Repeats, opt.StageDuration, overhead, exploreNeed.Round(time.Second))
	fmt.Fprintf(stdout, "  4. Campaign at the highest passing level with sample-sized observation (%s cap, including six network preflights and drains).\n", *budget)
	fmt.Fprintln(stdout, capacityStepDownPlanLine(*budget))
	if *extended {
		fmt.Fprintf(stdout, "  Explore and campaign use the extended envelope (target class %s); uncalibrated levels are never probed.\n", *targetClass)
	}
	fmt.Fprintln(stdout, "  5. Offline audit and executive report.")
	fmt.Fprintf(stdout, "Total planned wall time at most %s.\n", config.NetworkBaselineWallBudget+calWall+*exploreBudget+time.Duration(1+capacityMaxStepDowns)*(*budget)+time.Duration(capacityMaxStepDowns)*capacityRecoveryMaxWait)
	fmt.Fprintf(stdout, "  The total reserves %d step-down campaigns and %d recovery gates of at most %s each; the bisection stops early when it converges or a campaign ends without a PASS or a latency or error FAIL.\n", capacityMaxStepDowns, capacityMaxStepDowns, capacityRecoveryMaxWait)
	fmt.Fprintf(stdout, "  Protocol note: explore stages last %s each while campaigns sustain each level much longer; short stages can overestimate sustained capacity.\n", opt.StageDuration)
	if err := capacityRateWarning(stdout, *referenceReport, *maxUsers, scenario.MaxRPS); err != nil {
		return fail(err)
	}
	if *dry && *existing == "" {
		if *hook != "" {
			scenario.PerVUCookieEnvPrefix = ""
		}
		if _, err := config.Parse(scenario, config.Options{Authorized: true, NetworkBaseline: true, ExtendedEnvelope: extendedRequest, AllowInsecureHTTP: *insecureHTTP}); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "Zero-error sample floor: %d per series. Window = ceil(samples / slowest measured series rate); observation = three full windows. Supply --explore-file for the numeric estimate.\n", sampleFloor)
		return 0
	}
	if !*dry {
		if err := prepareCapacityDirectory(*dir, *resume, capacityManifest{
			ScenarioSHA: digest, MaxUsers: *maxUsers, Budget: *budget,
			ExploreBudget: *exploreBudget, LongRun: *long, Calibration: *calibration, Mirror: mirror,
		}); err != nil {
			return fail(err)
		}
		lock := filepath.Join(*dir, "active")
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fail(errors.New("capacity directory is locked; after confirming no process is active, remove its active marker"))
		}
		f.Close()
		defer os.Remove(lock)
		if *resume {
			state, err := completedCapacity(*dir, *budget)
			if err != nil {
				return fail(err)
			}
			if state != "" {
				fmt.Fprintf(stdout, "Reused completed audit and executive report. Assessment: %s.\n", state)
				return 0
			}
		}
	}
	if *hook != "" && !*dry {
		restore, err := provisionCapacityCookies(*hook, hookArgs, scenario.PerVUCookieEnvPrefix, *maxUsers)
		if err != nil {
			return fail(err)
		}
		defer restore()
	}
	exploreScenario := scenario
	exploreScenario.Users = opt.MinUsers
	// Explore judges each stage on its aggregate; observation windows are a
	// campaign control and would split short stages below the sample floor.
	exploreScenario.WindowDuration = ""
	v, err := config.Parse(exploreScenario, config.Options{Authorized: true, NetworkBaseline: true, ExtendedEnvelope: extendedRequest, AllowInsecureHTTP: *insecureHTTP})
	if err != nil {
		return fail(err)
	}
	var explorePath, campaignPath string
	exploreCalibration := *calibration
	if exploreCalibration == "" {
		exploreCalibration = filepath.Join(*dir, "selfcal-explore.json")
	}
	exploreFile := *file
	if *dry {
		explorePath = *existing
	} else {
		explorePath = filepath.Join(*dir, "explore.json")
		campaignPath = filepath.Join(*dir, "campaign.json")
		exploreFile = filepath.Join(*dir, "explore-scenario.json")
		if err := report.WriteAnyJSON(exploreFile, exploreScenario); err != nil {
			return fail(err)
		}
		if *resume {
			if err := archiveInterruptedCapacity(explorePath, campaignPath); err != nil {
				return fail(err)
			}
		}
		if _, err := os.Stat(explorePath); os.IsNotExist(err) {
			if err := capacityPreflight(stdout, v, *calibration); err != nil {
				return fail(err)
			}
			if *calibration == "" {
				if code := ensureCapacityCalibration(exploreCalibration, exploreFile, v, calibrationLevels, mirror, stdout, stderr); code != 0 {
					return code
				}
			}
			if *extended {
				if _, code := gateExtendedLevels(&v, exploreCalibration, calibrationLevels, stdout, stderr); code != 0 {
					return code
				}
			}
			exploreArgs := []string{
				"--config", exploreFile, "--min-users", strconv.Itoa(opt.MinUsers), "--max-users", strconv.Itoa(*maxUsers),
				"--budget", exploreBudget.String(), "--stage-duration", opt.StageDuration.String(), "--authorized", "--out", explorePath,
				"--calibration", exploreCalibration, "--require-calibration",
			}
			exploreArgs = append(exploreArgs, capacityTransportArgs(*insecureHTTP)...)
			code := explore(append(exploreArgs, envelopeArgs...), stdout, stderr)
			if code == 2 {
				return code
			}
		}
	}
	result, err := readCapacityExplore(explorePath)
	if err != nil {
		return fail(err)
	}
	users, window, err := capacityObservation(stdout, result, v, *maxUsers)
	if err != nil {
		return fail(err)
	}
	capacityObservedRateWarning(stdout, result, users, scenario.MaxRPS)
	required, err := capacityRequiredSamples(v)
	if err != nil {
		return fail(err)
	}
	baselineAllowance := 6 * config.NetworkBaselineWallBudget
	// planLevel sizes one campaign level from its own explore delivery rates.
	planLevel := func(level int, window time.Duration) (campaign.Options, error) {
		options := campaignOptions
		options.Observation = max(10*time.Second, 3*window)
		options.Recovery = max(capacityTiming.Recovery, window)
		var floor time.Duration
		var cycle float64
		options.Warmup, floor, cycle, options.WarmupSettling = capacityWarmup(result, level, v.RampUp, window)
		schedule := options
		schedule.Budget = 0
		// Shrink a sized warm-up toward its settling floor only as far as
		// the wall cap requires; the settling check still verifies it.
		for options.Warmup-options.WarmupSettling.Window() >= floor && campaign.PlannedBudget(schedule)+baselineAllowance > options.Budget {
			options.Warmup -= options.WarmupSettling.Window()
			schedule.Warmup = options.Warmup
		}
		minimumBudget := campaign.PlannedBudget(schedule) + baselineAllowance
		fmt.Fprintf(stdout, "Minimum campaign wall budget at %d VUs including six network preflights and bounded drains: %s.\n", level, minimumBudget)
		// A default (not user-set) campaign cap grows to the computed minimum
		// as long as it stays within the campaign wall-time limit.
		if options.Budget < minimumBudget && !explicitCampaignBudget && minimumBudget <= capacityCampaignBudgetLimit(options) {
			fmt.Fprintf(stdout, "Campaign wall cap raised from the %s default to %s at %d VUs (within the %s limit).\n", options.Budget, minimumBudget, level, capacityCampaignBudgetLimit(options))
			options.Budget = minimumBudget
		}
		if options.Budget < minimumBudget {
			return options, fmt.Errorf("budget %s is below %s; increase --budget within its ceiling, use explicit long-run staging opt-in if needed, simplify the workload, or validate more VUs", *budget, minimumBudget)
		}
		if err := campaign.Validate(options); err != nil {
			return options, fmt.Errorf("%w; increase --budget within its ceiling, use explicit long-run staging opt-in if needed, simplify the workload, or validate more VUs", err)
		}
		fmt.Fprintf(stdout, "Campaign at %d VUs: two trials of %s warm-up, %s observation and %s recovery, using %s windows and a %s wall cap.\n",
			level, options.Warmup, options.Observation, options.Recovery, window, *budget)
		fmt.Fprintln(stdout, capacityWarmupPlanLine(options, v.RampUp, cycle, floor))
		return options, nil
	}
	firstOptions, err := planLevel(users, window)
	if err != nil {
		return fail(err)
	}
	stepDowns := capacityStepDownLevels(result, users, 0)
	if *dry {
		if len(stepDowns) > 0 {
			fmt.Fprintf(stdout, "Step-down candidates if the %d VU campaign FAILs on latency or errors: %v (repeatedly passing explore levels, highest first).\n", users, stepDowns)
		} else {
			fmt.Fprintf(stdout, "No lower repeatedly passing explore level is available for a step-down below %d VUs.\n", users)
		}
		return 0
	}
	// runLevel runs (or reuses) one sustained campaign and verifies that the
	// saved evidence matches its planned controls.
	runLevel := func(index, level int, window time.Duration, options campaign.Options) (campaign.Result, string, int, error) {
		campaignFile, scenarioFile, calibrationFile := capacityCampaignFiles(index, level)
		path := filepath.Join(*dir, campaignFile)
		levelScenario := scenario
		levelScenario.Users = level
		levelScenario.WindowDuration = window.String()
		if v.Duration < window {
			levelScenario.Duration = window.String()
		}
		campaignScenario, err := config.Parse(levelScenario, config.Options{Authorized: true, NetworkBaseline: true, ExtendedEnvelope: extendedRequest, AllowInsecureHTTP: *insecureHTTP})
		if err != nil {
			return campaign.Result{}, path, 2, err
		}
		if err := campaign.ValidateForScenario(campaignScenario, options); err != nil {
			return campaign.Result{}, path, 2, err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			derived := filepath.Join(*dir, scenarioFile)
			if err := report.WriteAnyJSON(derived, levelScenario); err != nil {
				return campaign.Result{}, path, 2, err
			}
			campaignArgs := []string{
				"--config", derived, "--authorized", "--out", path,
				"--warmup", options.Warmup.String(), "--observation", options.Observation.String(),
				"--recovery", options.Recovery.String(), "--budget", budget.String(),
			}
			campaignArgs = append(campaignArgs, warmupSettlingArgs(options.WarmupSettling)...)
			campaignCalibration := *calibration
			if *extended {
				campaignCalibration = exploreCalibration
			}
			if campaignCalibration == "" {
				campaignCalibration = filepath.Join(*dir, calibrationFile)
				if code := ensureCapacityCalibration(campaignCalibration, derived, campaignScenario, []int{level}, mirror, stdout, stderr); code != 0 {
					return campaign.Result{}, path, code, nil
				}
			}
			campaignArgs = append(campaignArgs, "--calibration", campaignCalibration, "--require-calibration")
			campaignArgs = append(campaignArgs, capacityTransportArgs(*insecureHTTP)...)
			campaignArgs = append(campaignArgs, envelopeArgs...)
			if *long {
				campaignArgs = append(campaignArgs, "--allow-long-run", "--staging-confirmed")
			}
			if code := campaignCmd(campaignArgs, stdout, stderr); code == 2 {
				return campaign.Result{}, path, code, nil
			}
		}
		evidence, err := capacityplan.ReadCampaign(path)
		if err != nil {
			return evidence, path, 2, err
		}
		if evidence.ExecutionState == "RUNNING" || evidence.Outcome == "ABORTED" {
			return evidence, path, 2, errors.New("preserved interrupted campaign; use --resume --authorized to repeat it with a fresh schedule")
		}
		if evidence.BaseUsers != level || evidence.Observation != options.Observation.String() ||
			evidence.Warmup != options.Warmup.String() || !sameWarmupSettling(evidence.WarmupSettling, options.WarmupSettling) ||
			evidence.Recovery != options.Recovery.String() || evidence.Budget != budget.String() ||
			evidence.Objectives == nil || evidence.Objectives.WindowDuration != window.String() ||
			evidence.Objectives.MaxJourneyErrorRate != v.Scenario.MaxErrorRate {
			return evidence, path, 2, errors.New("saved campaign does not match the planned controls")
		}
		for _, trial := range evidence.Trials {
			for _, phase := range trial.Phases {
				if phase.Run.Config.P95LimitMS != v.Scenario.P95LimitMS ||
					phase.Run.Target != v.Base.Scheme+"://"+v.Base.Host ||
					phase.Run.Config.WorkloadFingerprint != campaignScenario.WorkloadFingerprint ||
					phase.Run.Config.WorkloadRevision != v.Scenario.WorkloadRevision ||
					phase.Run.Config.ApplicationRevision != v.Scenario.ApplicationRevision ||
					phase.Run.Config.Environment != v.Scenario.Environment {
					return evidence, path, 2, errors.New("saved campaign phase does not match the declared target and revisions")
				}
			}
		}
		return evidence, path, 0, nil
	}
	ladderRecord := capacityLadder{SchemaVersion: 1, ExploreStage: opt.StageDuration.String(), MaxStepDowns: capacityMaxStepDowns}
	results := map[string]campaign.Result{}
	levelUsers := map[string]int{}
	evidence, path, code, err := runLevel(0, users, window, firstOptions)
	if err != nil {
		return fail(err)
	}
	if code != 0 {
		return code
	}
	ladderRecord.Entries = append(ladderRecord.Entries, capacityLadderEntry{Users: users, Campaign: filepath.Base(path), Outcome: evidence.Outcome})
	results[filepath.Base(path)], levelUsers[filepath.Base(path)] = evidence, users
	bisection := newCapacityBisection(stepDowns, users)
	recoveryReference := capacityRecoveryReference(result)
	searching := capacityStepDownAllowed(evidence)
	for i := 0; i < capacityMaxStepDowns && searching; i++ {
		level, ok := bisection.next()
		if !ok {
			break
		}
		fmt.Fprintf(stdout, "Sustained campaign at %d VUs FAILED on latency or errors although it passed the short explore stages; next sustained campaign at %d VUs by bisection (%d of at most %d).\n",
			ladderRecord.Entries[0].Users, level, i+1, capacityMaxStepDowns)
		levelWindow, err := capacityObservationAt(stdout, result, v, *maxUsers, level, required)
		var options campaign.Options
		if err == nil {
			options, err = planLevel(level, levelWindow)
		}
		if err != nil {
			// The level cannot be measured within the per-window sample floor,
			// the 60s window ceiling, or the wall budget. Lower levels deliver
			// fewer samples per second, so exclude them too and keep searching
			// the remaining higher candidates instead of stopping the ladder.
			ladderRecord.Entries = append(ladderRecord.Entries, capacityLadderEntry{Users: level, Outcome: "NOT_RUN", Reason: err.Error() + "; lower candidates need even longer windows and were skipped"})
			fmt.Fprintf(stdout, "Campaign at %d VUs NOT_RUN: %v; skipping it and lower candidates, continuing the bisection above it.\n", level, err)
			bisection.exclude(level)
			continue
		}
		var recovery *capacityRecoveryCheck
		campaignFile, _, _ := capacityCampaignFiles(i+1, level)
		if _, statErr := os.Stat(filepath.Join(*dir, campaignFile)); os.IsNotExist(statErr) {
			fmt.Fprintf(stdout, "Recovery gate before %d VUs: %s cooldown, then network-path probes until p50 returns near the pre-load reference (at most %s).\n", level, capacityRecoveryCooldown, capacityRecoveryMaxWait)
			check := capacityRecoveryGate(context.Background(), v.NetworkBaseline, recoveryReference)
			recovery = &check
			fmt.Fprintf(stdout, "Recovery gate: %s after %s.\n", check.Status, check.Waited)
			if check.Status == capacityRecoveryNotRecovered {
				ladderRecord.Entries = append(ladderRecord.Entries, capacityLadderEntry{Users: level, Outcome: capacityOutcomeNotRecovered, Reason: check.Reason, Recovery: recovery})
				ladderRecord.StopReason = "target did not recover; later campaigns would be contaminated"
				break
			}
		}
		evidence, path, code, err = runLevel(i+1, level, levelWindow, options)
		if err != nil {
			return fail(err)
		}
		if code != 0 {
			return code
		}
		ladderRecord.Entries = append(ladderRecord.Entries, capacityLadderEntry{Users: level, Campaign: filepath.Base(path), Outcome: evidence.Outcome, Recovery: recovery})
		results[filepath.Base(path)], levelUsers[filepath.Base(path)] = evidence, level
		// Only target verdicts steer the bisection; INCONCLUSIVE or ABORTED
		// evidence stops the ladder.
		searching = evidence.ExecutionState == "COMPLETED" && (evidence.Outcome == "PASS" || evidence.Outcome == "FAIL")
		if searching {
			bisection.record(level, evidence.Outcome == "PASS")
		}
	}
	finishCapacityLadder(&ladderRecord)
	stepped := len(ladderRecord.Entries) > 1
	if stepped {
		if err := report.WriteAnyJSON(filepath.Join(*dir, capacityLadderFile), ladderRecord); err != nil {
			return fail(err)
		}
		for _, line := range capacityLadderSummary(ladderRecord) {
			fmt.Fprintln(stdout, line)
		}
	}
	campaignPath = filepath.Join(*dir, ladderRecord.AuditedCampaign)
	evidence, users = results[ladderRecord.AuditedCampaign], levelUsers[ladderRecord.AuditedCampaign]
	reportPath := filepath.Join(*dir, "capacity.md")
	htmlPath := filepath.Join(*dir, "capacity.html")
	auditJSON := filepath.Join(*dir, "audit.json")
	auditReportPath, auditHTMLPath := reportPath, htmlPath
	if *intensity > 0 || stepped {
		auditReportPath = filepath.Join(*dir, "capacity-audit.md")
	}
	if stepped {
		auditHTMLPath = filepath.Join(*dir, "capacity-audit.html")
	}
	if _, err := os.Stat(auditReportPath); os.IsNotExist(err) {
		auditArgs := []string{
			"--campaign", campaignPath, "--peak-users", strconv.Itoa(users), "--out", auditJSON,
			"--report-out", auditReportPath, "--html-out", auditHTMLPath, "--explore-file", explorePath,
		}
		if *intensity > 0 {
			auditArgs = append(auditArgs, "--actions-per-user-minute", strconv.FormatFloat(*intensity, 'f', -1, 64))
		}
		code := auditCmd(auditArgs, stdout, stderr)
		if code != 0 {
			return code
		}
	} else if err != nil {
		return fail(err)
	}
	markdownSource := auditReportPath
	if *intensity > 0 {
		destination := reportPath
		if stepped {
			destination = filepath.Join(*dir, "capacity-intensity.md")
			markdownSource = destination
		}
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			if err := appendIntensityHypothesis(auditReportPath, destination, evidence, *intensity); err != nil {
				return fail(err)
			}
		}
	}
	if stepped {
		if err := writeCapacityLadderReports(markdownSource, auditHTMLPath, reportPath, htmlPath, ladderRecord); err != nil {
			return fail(err)
		}
	}
	return 0
}

func completedCapacity(dir string, budget time.Duration) (string, error) {
	reportPath := filepath.Join(dir, "capacity.md")
	data, err := readCapacityFile(reportPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil || len(data) == 0 {
		return "", errors.New("saved executive report cannot be reused")
	}
	audited := "campaign.json"
	ladder, err := readCapacityLadder(dir)
	if err != nil {
		return "", err
	}
	if ladder != nil {
		audited = ladder.AuditedCampaign
	}
	evidence, digest, err := capacityplan.ReadCampaignWithFileSHA256(filepath.Join(dir, audited))
	if err != nil || evidence.ExecutionState != "COMPLETED" || evidence.Budget != budget.String() {
		return "", errors.New("completed campaign does not match the requested resume controls")
	}
	auditData, err := readCapacityFile(filepath.Join(dir, "audit.json"))
	if err != nil {
		return "", err
	}
	var assessment capacityplan.Assessment
	if err := json.Unmarshal(auditData, &assessment); err != nil || assessment.State == "" ||
		assessment.Audit.SourceFileSHA256 != digest {
		return "", errors.New("saved audit does not match the completed campaign bytes")
	}
	return assessment.State, nil
}

func capacityPreflight(stdout io.Writer, v config.Validated, calibrationPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Native load admission uses Duration; zero leaves only the bounded
	// pre-load baseline. The synthetic regression verifies zero load starts.
	v.Duration = 0
	result := engine.Run(ctx, v)
	if ctx.Err() != nil || result.Metrics.Total != 0 || result.IterationsStarted != 0 {
		return errors.New("network preflight was interrupted or produced unexpected load")
	}
	writeNetworkPlacement(stdout, result)
	if issue := report.NetworkBaselineIssue(result); issue != "" {
		return errors.New(issue)
	}
	if calibrationPath != "" {
		_, err := loadAnyCalibration(calibrationPath)
		return err
	}
	return nil
}

func archiveInterruptedCapacity(explorePath, campaignPath string) error {
	exploration, err := readCapacityExplore(explorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && exploration.Outcome == "ABORTED" {
		if _, err := os.Stat(campaignPath); !os.IsNotExist(err) {
			return errors.New("cannot resume an interrupted exploration with existing campaign evidence")
		}
		return os.Rename(explorePath, explorePath+"."+strconv.FormatInt(time.Now().UnixNano(), 10)+".interrupted")
	}
	stepDowns, _ := filepath.Glob(filepath.Join(filepath.Dir(campaignPath), "campaign-stepdown-*.json"))
	for _, path := range append([]string{campaignPath}, stepDowns...) {
		evidence, err := capacityplan.ReadCampaign(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if evidence.ExecutionState == "RUNNING" || evidence.Outcome == "ABORTED" {
			if err := os.Rename(path, path+"."+strconv.FormatInt(time.Now().UnixNano(), 10)+".interrupted"); err != nil {
				return err
			}
		}
	}
	return nil
}

func prepareCapacityDirectory(dir string, resume bool, manifest capacityManifest) error {
	path := filepath.Join(dir, "protocol.json")
	if !resume {
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		return report.WriteAnyJSON(path, manifest)
	}
	data, err := readCapacityFile(path)
	if err != nil {
		return err
	}
	var saved capacityManifest
	if err := json.Unmarshal(data, &saved); err != nil {
		return errors.New("saved capacity manifest is malformed")
	}
	if manifest.Budget < saved.Budget || (saved.LongRun && !manifest.LongRun) {
		return errors.New("resume cannot reduce the saved execution limits")
	}
	// More time may be opted into after an evidence-based forecast. Completed
	// campaign reports are independently matched to the exact requested budget.
	saved.Budget = manifest.Budget
	saved.LongRun = manifest.LongRun
	if saved != manifest {
		return errors.New("resume settings or scenario contents differ from the saved protocol")
	}
	return nil
}

// capacityTransportArgs forwards the explicit plaintext HTTP opt-in to every
// traffic stage so the protocol never silently differs from its plan.
func capacityTransportArgs(allowInsecureHTTP bool) []string {
	if allowInsecureHTTP {
		return []string{"--allow-insecure-http"}
	}
	return nil
}

// capacityCampaignBudgetLimit is the largest wall cap campaign.Validate
// accepts for these options.
func capacityCampaignBudgetLimit(options campaign.Options) time.Duration {
	if options.LongRun {
		return campaign.LongRunWallTimeLimit
	}
	return campaign.DefaultWallTimeLimit
}
