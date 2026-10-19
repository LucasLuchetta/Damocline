package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaignprofile"
	"github.com/LucasLuchetta/Damocline/internal/config"
)

func campaignProfileCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, campaignProfileUsage)
		return 2
	}
	store := campaignprofile.Store{Dir: filepath.Join(".damocline", "profiles")}
	switch args[0] {
	case "save":
		fs := flag.NewFlagSet("campaign profile save", flag.ContinueOnError)
		fs.SetOutput(stderr)
		file := fs.String("file", "", "profile JSON to import")
		replace := fs.Bool("replace", false, "replace an existing profile with the same name")
		if fs.Parse(args[1:]) != nil || *file == "" || fs.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: damocline campaign profile save --file profile.json [--replace]")
			return 2
		}
		profile, migrated, err := store.Import(*file)
		if err != nil {
			fmt.Fprintf(stderr, "profile import error: %v\n", err)
			return 2
		}
		if err := store.Save(profile, *replace); err != nil {
			fmt.Fprintf(stderr, "profile save error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Saved profile %q in private local storage", profile.Name)
		if migrated {
			fmt.Fprint(stdout, " (migrated to current schema)")
		}
		fmt.Fprintln(stdout, ".")
		return 0
	case "list":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: damocline campaign profile list")
			return 2
		}
		profiles, err := store.List()
		if err != nil {
			fmt.Fprintf(stderr, "profile list error: %v\n", err)
			return 2
		}
		if len(profiles) == 0 {
			fmt.Fprintln(stdout, "No saved campaign profiles.")
			return 0
		}
		for _, p := range profiles {
			fmt.Fprintf(stdout, "%-48s %s\n", p.Name, safeHistoryField(p.Description))
		}
		return 0
	case "show":
		fs := flag.NewFlagSet("campaign profile show", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "profile name")
		if fs.Parse(args[1:]) != nil || *name == "" || fs.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: damocline campaign profile show --name NAME")
			return 2
		}
		p, _, err := store.Load(*name)
		if err != nil {
			fmt.Fprintf(stderr, "profile load error: %v\n", err)
			return 2
		}
		o, _ := p.CampaignOptions()
		fmt.Fprintf(stdout, "Name: %s\nDescription: %s\nSchema: %d\nScenario: %s\nBase users: %d\nObservation: %s\nRepeats: %d\nBudget: %s\n", p.Name, safeHistoryField(p.Description), p.SchemaVersion, safeHistoryField(p.Scenario.Name), p.Scenario.Users, o.Observation, o.Repeats, o.Budget)
		fmt.Fprintln(stdout, "Scenario endpoints and credentials are omitted.")
		return 0
	case "migrate":
		fs := flag.NewFlagSet("campaign profile migrate", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "profile name")
		if fs.Parse(args[1:]) != nil || *name == "" || fs.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: damocline campaign profile migrate --name NAME")
			return 2
		}
		changed, err := store.Migrate(*name)
		if err != nil {
			fmt.Fprintf(stderr, "profile migration error: %v\n", err)
			return 2
		}
		if changed {
			fmt.Fprintf(stdout, "Migrated profile %q to schema %d.\n", *name, campaignprofile.CurrentSchemaVersion)
		} else {
			fmt.Fprintf(stdout, "Profile %q already uses the current schema.\n", *name)
		}
		return 0
	case "delete":
		fs := flag.NewFlagSet("campaign profile delete", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "profile name")
		if fs.Parse(args[1:]) != nil || *name == "" || fs.NArg() != 0 {
			fmt.Fprintln(stderr, "usage: damocline campaign profile delete --name NAME")
			return 2
		}
		if err := store.Delete(*name); err != nil {
			fmt.Fprintf(stderr, "profile delete error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Deleted profile %q.\n", *name)
		return 0
	case "run":
		return campaignProfileRun(args[1:], store, stdout, stderr)
	case "verify":
		return campaignProfileVerifyCmd(args[1:], store, stdout, stderr)
	default:
		fmt.Fprintln(stderr, campaignProfileUsage)
		return 2
	}
}

const campaignProfileUsage = `Usage: damocline campaign profile <save|list|show|migrate|delete|run|verify>
  save --file profile.json [--replace]
  list
  show --name NAME
  migrate --name NAME
  delete --name NAME
  run --name NAME (--authorized | --dry-run) [--allow-writes] [--allow-insecure-http] [--staging-confirmed] [--calibration FILE] [--out FILE]
  verify --name NAME --campaign FILE [--out FILE]`

func campaignProfileRun(args []string, store campaignprofile.Store, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("campaign profile run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "saved profile name")
	authorized := fs.Bool("authorized", false, "confirm authorization for this execution")
	dry := fs.Bool("dry-run", false, "validate without sending traffic")
	writes := fs.Bool("allow-writes", false, "opt in to HTTP mutation methods for this execution")
	insecure := fs.Bool("allow-insecure-http", false, "permit plaintext HTTP to a non-loopback target for this execution")
	staging := fs.Bool("staging-confirmed", false, "confirm staging environment for long-run execution")
	calibration := fs.String("calibration", "", "local calibration record for this execution")
	out := fs.String("out", "", "campaign report output path")
	metrics := fs.String("app-metrics-endpoint", "", "authorized Prometheus-text endpoint for this execution")
	metricsInterval := fs.Duration("app-metrics-interval", 10*time.Second, "application metrics interval")
	if fs.Parse(args) != nil || *name == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: damocline campaign profile run --name NAME (--authorized | --dry-run) [runtime opt-ins]")
		return 2
	}
	profile, _, err := store.Load(*name)
	if err != nil {
		fmt.Fprintf(stderr, "profile load error: %v\n", err)
		return 2
	}
	if !*dry && !*authorized {
		fmt.Fprintln(stderr, "profile execution requires --authorized for this run")
		return 2
	}
	if profile.Options.LongRun && !*staging && !*dry {
		fmt.Fprintln(stderr, "long-run profile execution requires --staging-confirmed for this run")
		return 2
	}
	if profile.Options.RequireCalibration && *calibration == "" && !*dry {
		fmt.Fprintln(stderr, "profile requires --calibration for this run")
		return 2
	}
	if *out == "" && !*dry {
		if err := os.MkdirAll(filepath.Join(".damocline", "reports"), 0700); err != nil {
			fmt.Fprintf(stderr, "report directory error: %v\n", err)
			return 2
		}
		*out = filepath.Join(".damocline", "reports", profile.Name+"-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	}
	if *out != "" && !*dry {
		if _, err := os.Lstat(*out); err == nil {
			fmt.Fprintf(stderr, "campaign report path already exists: %s\n", *out)
			return 2
		} else if !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "cannot inspect campaign report path: %v\n", err)
			return 2
		}
	}
	data, err := os.CreateTemp("", "damocline-profile-scenario-*.json")
	if err != nil {
		fmt.Fprintf(stderr, "scenario temp file error: %v\n", err)
		return 2
	}
	path := data.Name()
	defer os.Remove(path)
	if err := data.Chmod(0600); err != nil {
		data.Close()
		fmt.Fprintf(stderr, "scenario temp file permission error: %v\n", err)
		return 2
	}
	if err := jsonEncodeScenario(data, profile.Scenario); err != nil {
		data.Close()
		fmt.Fprintf(stderr, "scenario encode error: %v\n", err)
		return 2
	}
	if err := data.Close(); err != nil {
		fmt.Fprintf(stderr, "scenario write error: %v\n", err)
		return 2
	}
	o, _ := profile.CampaignOptions()
	argsOut := []string{"--config", path, "--warmup", o.Warmup.String(), "--stabilization", o.Stabilization.String(), "--observation", o.Observation.String(), "--spike-users", fmt.Sprint(o.SpikeUsers), "--spike-duration", o.SpikeDuration.String(), "--recovery", o.Recovery.String(), "--soak", o.Soak.String(), "--cooldown", o.Cooldown.String(), "--repeats", fmt.Sprint(o.Repeats), "--budget", o.Budget.String(), "--stability-min-windows", fmt.Sprint(o.Stability.MinimumWindows), "--stability-min-samples", fmt.Sprint(o.Stability.MinimumSamplesPerWindow), "--stability-max-p95-increase-percent", fmt.Sprint(o.Stability.MaxP95IncreasePercent), "--stability-max-p95-increase-ms", fmt.Sprint(o.Stability.MaxP95IncreaseMS), "--stability-max-throughput-drop-percent", fmt.Sprint(o.Stability.MaxThroughputDropPercent), "--resource-guardrail-samples", fmt.Sprint(o.Guardrails.ConsecutiveSamples)}
	if o.Guardrails.MaxProcessCPUPercent > 0 {
		argsOut = append(argsOut, "--max-generator-cpu-percent", fmt.Sprint(o.Guardrails.MaxProcessCPUPercent))
	}
	if o.Guardrails.MaxProcessRSSBytes > 0 {
		argsOut = append(argsOut, "--max-generator-rss-mb", fmt.Sprint(o.Guardrails.MaxProcessRSSBytes/(1024*1024)))
	}
	if o.LongRun {
		argsOut = append(argsOut, "--allow-long-run")
	}
	if *staging || (o.LongRun && *dry) {
		argsOut = append(argsOut, "--staging-confirmed")
	}
	if *authorized {
		argsOut = append(argsOut, "--authorized")
	}
	if *dry {
		argsOut = append(argsOut, "--dry-run")
	}
	if *writes {
		argsOut = append(argsOut, "--allow-writes")
	}
	if *insecure {
		argsOut = append(argsOut, "--allow-insecure-http")
	}
	if *calibration != "" {
		argsOut = append(argsOut, "--calibration", *calibration)
	}
	if o.RequireCalibration {
		argsOut = append(argsOut, "--require-calibration")
	}
	if *out != "" {
		argsOut = append(argsOut, "--out", *out)
	}
	if *metrics != "" {
		argsOut = append(argsOut, "--app-metrics-endpoint", *metrics, "--app-metrics-interval", metricsInterval.String())
	}
	return campaignCmd(argsOut, stdout, stderr)
}

func jsonEncodeScenario(w io.Writer, scenario config.Scenario) error {
	return json.NewEncoder(w).Encode(scenario)
}
