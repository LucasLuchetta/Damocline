package cli

import (
	"context"
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
	var result explorer.Result
	data, err := readCapacityFile(path)
	if err == nil {
		err = explorer.ValidateArtifactJSON(data)
	}
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
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
	if result.Outcome == "ABORTED" {
		fmt.Fprintf(w, "Explore stopped early (ABORTED); continuing with the highest repeatedly passing level %d VUs. Levels above it were not explored.\n", *result.HighestPassingUsers)
	}
	if users > maxUsers || result.Repeats < 2 || result.Repeats > 4 {
		return 0, 0, errors.New("explore evidence is outside the declared capacity controls")
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
				return 0, 0, errors.New("explore trial does not match the scenario, target, or passing level")
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
					return 0, 0, errors.New("explore trial is missing a configured step")
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
		return 0, 0, errors.New("missing, zero, or invalid measured delivery rate for a required series")
	}
	fmt.Fprintf(w, "Slowest measured rate %.3f/s. Minimum window %.0fs; three-window observation %.0fs. Delivery may vary.\n", slowest, seconds, 3*seconds)
	if seconds > 60 {
		return 0, 0, errors.New("required window exceeds 60s; simplify the workload or validate more VUs within the supported ceiling")
	}
	return users, time.Duration(seconds) * time.Second, nil
}

func capacityPassingLevel(result explorer.Result, extended bool) int {
	if result.HighestPassingUsers != nil && (result.Outcome == "BRACKETED" || result.Outcome == "PASSED_TO_CONFIGURED_CEILING" || result.Outcome == "ABORTED") {
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
	opt := explorer.Options{MinUsers: min(2, *maxUsers), MaxUsers: *maxUsers, Repeats: 2, StageDuration: 20 * time.Second, Budget: *exploreBudget}
	if stage, ok := capacityExploreStage(scenario, opt.MinUsers, *referenceReport); ok {
		opt.StageDuration = stage
	}
	campaignOptions := campaign.Options{
		Warmup: 10 * time.Second, Recovery: 15 * time.Second, Observation: 45 * time.Second,
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
	// Size the explore budget from the actual stages: each planned level runs
	// opt.Repeats stages plus a bounded network preflight. A default budget
	// grows to fit (within the 15m explorer cap); an explicit budget that is
	// too small is refused before any traffic instead of aborting mid-ladder.
	exploreNeed := time.Duration(len(ladder)*opt.Repeats) * (opt.StageDuration + 5*time.Second)
	explicitExploreBudget := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "explore-budget" {
			explicitExploreBudget = true
		}
	})
	if exploreNeed > 15*time.Minute {
		return fail(fmt.Errorf("explore needs about %s for %d levels x %d repeats of %s stages, above the 15m explore cap; lower --max-users", exploreNeed.Round(time.Second), len(ladder), opt.Repeats, opt.StageDuration))
	}
	if *exploreBudget < exploreNeed {
		if explicitExploreBudget {
			return fail(fmt.Errorf("--explore-budget %s is too small for %d levels x %d repeats of %s stages; use at least %s", *exploreBudget, len(ladder), opt.Repeats, opt.StageDuration, exploreNeed.Round(time.Second)))
		}
		*exploreBudget = exploreNeed.Round(time.Second)
		opt.Budget = *exploreBudget
	}
	calibrationLevels := ladder
	if *extended {
		calibrationLevels = capacityCalibrationLevels(opt, *calLevel)
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
			calWall += campaignWall
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
			fmt.Fprintf(stdout, "  2. damocline calibrate for levels %v, then for the campaign level (%s, loopback mirror only, %s; up to %s).\n", ladder, source, envelope, calWall)
		}
	} else {
		fmt.Fprintf(stdout, "  2. Existing calibration artifact %s (no new calibration).\n", *calibration)
	}
	fmt.Fprintf(stdout, "  3. Explore levels %v with --calibration and --require-calibration (%s cap).\n", ladder, *exploreBudget)
	fmt.Fprintf(stdout, "  4. Campaign at the highest passing level with sample-sized observation (%s cap, including six network preflights and drains).\n", *budget)
	if *extended {
		fmt.Fprintf(stdout, "  Explore and campaign use the extended envelope (target class %s); uncalibrated levels are never probed.\n", *targetClass)
	}
	fmt.Fprintln(stdout, "  5. Offline audit and executive report.")
	fmt.Fprintf(stdout, "Total planned wall time at most %s.\n", config.NetworkBaselineWallBudget+calWall+*exploreBudget+*budget)
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
	campaignOptions.Observation = max(10*time.Second, 3*window)
	campaignOptions.Recovery = max(15*time.Second, window)
	baselineAllowance := 6 * config.NetworkBaselineWallBudget
	schedule := campaignOptions
	schedule.Budget = 0
	minimumBudget := campaign.PlannedBudget(schedule) + baselineAllowance
	fmt.Fprintf(stdout, "Minimum campaign wall budget including six network preflights and bounded drains: %s.\n", minimumBudget)
	if campaignOptions.Budget < minimumBudget {
		return fail(fmt.Errorf("budget %s is below %s; increase --budget within its ceiling, use explicit long-run staging opt-in if needed, simplify the workload, or validate more VUs", *budget, minimumBudget))
	}
	if err := campaign.Validate(campaignOptions); err != nil {
		return fail(fmt.Errorf("%w; increase --budget within its ceiling, use explicit long-run staging opt-in if needed, simplify the workload, or validate more VUs", err))
	}
	fmt.Fprintf(stdout, "Campaign at %d VUs: two trials of %s warm-up, %s observation and %s recovery, using %s windows and a %s wall cap.\n",
		users, campaignOptions.Warmup, campaignOptions.Observation, campaignOptions.Recovery, window, *budget)
	if *dry {
		return 0
	}
	scenario.Users = users
	scenario.WindowDuration = window.String()
	if v.Duration < window {
		scenario.Duration = window.String()
	}
	campaignScenario, err := config.Parse(scenario, config.Options{Authorized: true, NetworkBaseline: true, ExtendedEnvelope: extendedRequest, AllowInsecureHTTP: *insecureHTTP})
	if err != nil {
		return fail(err)
	}
	if err := campaign.ValidateForScenario(campaignScenario, campaignOptions); err != nil {
		return fail(err)
	}
	if _, err := os.Stat(campaignPath); os.IsNotExist(err) {
		derived := filepath.Join(*dir, "campaign-scenario.json")
		if err := report.WriteAnyJSON(derived, scenario); err != nil {
			return fail(err)
		}
		campaignArgs := []string{
			"--config", derived, "--authorized", "--out", campaignPath,
			"--warmup", campaignOptions.Warmup.String(), "--observation", campaignOptions.Observation.String(),
			"--recovery", campaignOptions.Recovery.String(), "--budget", budget.String(),
		}
		campaignCalibration := *calibration
		if *extended {
			campaignCalibration = exploreCalibration
		}
		if campaignCalibration == "" {
			campaignCalibration = filepath.Join(*dir, "selfcal-campaign.json")
			if code := ensureCapacityCalibration(campaignCalibration, derived, campaignScenario, []int{users}, mirror, stdout, stderr); code != 0 {
				return code
			}
		}
		campaignArgs = append(campaignArgs, "--calibration", campaignCalibration, "--require-calibration")
		campaignArgs = append(campaignArgs, capacityTransportArgs(*insecureHTTP)...)
		campaignArgs = append(campaignArgs, envelopeArgs...)
		if *long {
			campaignArgs = append(campaignArgs, "--allow-long-run", "--staging-confirmed")
		}
		if code := campaignCmd(campaignArgs, stdout, stderr); code == 2 {
			return code
		}
	}
	evidence, err := capacityplan.ReadCampaign(campaignPath)
	if err != nil {
		return fail(err)
	}
	if evidence.ExecutionState == "RUNNING" || evidence.Outcome == "ABORTED" {
		return fail(errors.New("preserved interrupted campaign; use --resume --authorized to repeat it with a fresh schedule"))
	}
	if evidence.BaseUsers != users || evidence.Observation != campaignOptions.Observation.String() ||
		evidence.Recovery != campaignOptions.Recovery.String() || evidence.Budget != budget.String() ||
		evidence.Objectives == nil || evidence.Objectives.WindowDuration != window.String() ||
		evidence.Objectives.MaxJourneyErrorRate != v.Scenario.MaxErrorRate {
		return fail(errors.New("saved campaign does not match the planned controls"))
	}
	for _, trial := range evidence.Trials {
		for _, phase := range trial.Phases {
			if phase.Run.Config.P95LimitMS != v.Scenario.P95LimitMS ||
				phase.Run.Target != v.Base.Scheme+"://"+v.Base.Host ||
				phase.Run.Config.WorkloadFingerprint != campaignScenario.WorkloadFingerprint ||
				phase.Run.Config.WorkloadRevision != v.Scenario.WorkloadRevision ||
				phase.Run.Config.ApplicationRevision != v.Scenario.ApplicationRevision ||
				phase.Run.Config.Environment != v.Scenario.Environment {
				return fail(errors.New("saved campaign phase does not match the declared target and revisions"))
			}
		}
	}
	reportPath := filepath.Join(*dir, "capacity.md")
	auditJSON := filepath.Join(*dir, "audit.json")
	auditReportPath := reportPath
	if *intensity > 0 {
		auditReportPath = filepath.Join(*dir, "capacity-audit.md")
	}
	if _, err := os.Stat(auditReportPath); os.IsNotExist(err) {
		code := auditCmd([]string{
			"--campaign", campaignPath, "--peak-users", strconv.Itoa(users), "--out", auditJSON,
			"--report-out", auditReportPath,
		}, stdout, stderr)
		if code != 0 {
			return code
		}
	} else if err != nil {
		return fail(err)
	}
	if *intensity > 0 {
		if err := appendIntensityHypothesis(auditReportPath, reportPath, evidence, *intensity); err != nil {
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
	evidence, digest, err := capacityplan.ReadCampaignWithFileSHA256(filepath.Join(dir, "campaign.json"))
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
	evidence, err := capacityplan.ReadCampaign(campaignPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if evidence.ExecutionState == "RUNNING" || evidence.Outcome == "ABORTED" {
		return os.Rename(campaignPath, campaignPath+"."+strconv.FormatInt(time.Now().UnixNano(), 10)+".interrupted")
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
