package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LucasLuchetta/Damocline/internal/campaign"
	"github.com/LucasLuchetta/Damocline/internal/capacityplan"
)

func protocolCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: damocline protocol seal|assess [flags]")
		return 2
	}
	switch args[0] {
	case "seal":
		fs := flag.NewFlagSet("protocol seal", flag.ContinueOnError)
		fs.SetOutput(stderr)
		input := fs.String("file", "", "unsealed protocol JSON")
		output := fs.String("out", "", "sealed protocol JSON output")
		if fs.Parse(args[1:]) != nil || len(fs.Args()) != 0 || *input == "" || *output == "" {
			fmt.Fprintln(stderr, "usage: damocline protocol seal --file protocol-draft.json --out protocol.json")
			return 2
		}
		if err := ensureProtocolOutputDistinct(*output, []string{*input}); err != nil {
			fmt.Fprintf(stderr, "invalid sealed protocol output: %v\n", err)
			return 2
		}
		data, err := os.ReadFile(*input)
		var protocol capacityplan.CapacityProtocol
		if err == nil {
			err = json.Unmarshal(data, &protocol)
		}
		if err != nil {
			fmt.Fprintf(stderr, "cannot read protocol: %v\n", err)
			return 2
		}
		sealed, err := capacityplan.SealProtocol(protocol, time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "invalid protocol: %v\n", err)
			return 2
		}
		if err := writeProtocolJSON(*output, sealed); err != nil {
			fmt.Fprintf(stderr, "cannot write sealed protocol: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Sealed protocol: %s\nSaved before campaign traffic: yes (enforced during assess)\n", sealed.ProtocolID)
		return 0
	case "assess":
		fs := flag.NewFlagSet("protocol assess", flag.ContinueOnError)
		fs.SetOutput(stderr)
		protocolPath := fs.String("file", "", "sealed protocol JSON")
		var explorations, confirmations, failures stringList
		fs.Var(&explorations, "exploration-campaign", "separate exploratory campaign JSON (repeatable)")
		fs.Var(&confirmations, "confirmation-campaign", "independent confirmation campaign JSON (repeatable)")
		fs.Var(&failures, "failure-campaign", "independent predeclared failure campaign JSON (repeatable)")
		out := fs.String("out", "", "assessment JSON output")
		markdown := fs.String("markdown", "", "executive and technical Markdown report")
		bundlePath := fs.String("bundle", "", "local reproduction bundle JSON")
		if fs.Parse(args[1:]) != nil || len(fs.Args()) != 0 || *protocolPath == "" || len(confirmations) == 0 {
			fmt.Fprintln(stderr, "usage: damocline protocol assess --file protocol.json --confirmation-campaign confirm-1.json --confirmation-campaign confirm-2.json [--out assessment.json --markdown report.md --bundle reproduction.json]")
			return 2
		}
		allPaths := append(append(append([]string{}, explorations...), confirmations...), failures...)
		if len(allPaths) > capacityplan.MaxCampaignCollectionSize {
			fmt.Fprintf(stderr, "protocol assessment is limited to %d campaign artifacts\n", capacityplan.MaxCampaignCollectionSize)
			return 2
		}
		inputPaths := append([]string{*protocolPath}, allPaths...)
		outputs := []string{*out, *markdown, *bundlePath}
		for i, output := range outputs {
			if output == "" {
				continue
			}
			if err := ensureProtocolOutputDistinct(output, append(append([]string{}, inputPaths...), outputs[:i]...)); err != nil {
				fmt.Fprintf(stderr, "invalid output path: %v\n", err)
				return 2
			}
		}
		protocolData, err := os.ReadFile(*protocolPath)
		var protocol capacityplan.CapacityProtocol
		if err == nil {
			err = json.Unmarshal(protocolData, &protocol)
		}
		if err != nil {
			fmt.Fprintf(stderr, "cannot read protocol: %v\n", err)
			return 2
		}
		read := func(paths []string) ([]campaign.Result, error) {
			results := make([]campaign.Result, 0, len(paths))
			for _, path := range paths {
				result, readErr := capacityplan.ReadCampaign(path)
				if readErr != nil {
					return nil, readErr
				}
				results = append(results, result)
			}
			return results, nil
		}
		explore, err := read(explorations)
		if err != nil {
			fmt.Fprintf(stderr, "cannot read exploration campaigns: %v\n", err)
			return 2
		}
		confirm, err := read(confirmations)
		if err != nil {
			fmt.Fprintf(stderr, "cannot read confirmation campaigns: %v\n", err)
			return 2
		}
		failed, err := read(failures)
		if err != nil {
			fmt.Fprintf(stderr, "cannot read failure campaigns: %v\n", err)
			return 2
		}
		assessment, err := capacityplan.AssessConfirmations(protocol, explore, confirm, failed)
		if err != nil {
			fmt.Fprintf(stderr, "cannot assess protocol: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Protocol assessment: %s | candidate %d VUs | passing campaigns %d | observed failing campaigns %d\n", assessment.State, assessment.CandidateUsers, assessment.ConfirmedPassingCampaigns, assessment.ObservedFailingCampaigns)
		artifacts := make([]capacityplan.CampaignArtifact, 0, len(allPaths))
		allCampaigns := append(append(append([]campaign.Result{}, explore...), confirm...), failed...)
		roles := append(append(append([]string{}, protocolRoles("exploration", len(explorations))...), protocolRoles("confirmation", len(confirmations))...), protocolRoles("failure_confirmation", len(failures))...)
		for i, result := range allCampaigns {
			artifacts = append(artifacts, capacityplan.CampaignArtifact{Role: roles[i], Path: allPaths[i], EvidenceID: assessment.EvidenceIDs[i], Result: result})
		}
		commands := []string{protocolReplayCommand(*protocolPath, explorations, confirmations, failures, *out, *markdown, *bundlePath)}
		if *out != "" {
			if err := writeProtocolJSON(*out, capacityplan.BuildProtocolReport(protocol, assessment, artifacts)); err != nil {
				fmt.Fprintf(stderr, "cannot write assessment JSON: %v\n", err)
				return 2
			}
		}
		if *markdown != "" {
			if err := os.WriteFile(*markdown, capacityplan.RenderProtocolReport(protocol, assessment, allCampaigns, Version, commands), 0600); err != nil {
				fmt.Fprintf(stderr, "cannot write Markdown report: %v\n", err)
				return 2
			}
		}
		if *bundlePath != "" {
			bundle, buildErr := capacityplan.BuildReproductionBundle(protocol, assessment, artifacts, Version, commands)
			if buildErr != nil {
				fmt.Fprintf(stderr, "cannot build reproduction bundle: %v\n", buildErr)
				return 2
			}
			if err := os.WriteFile(*bundlePath, bundle, 0600); err != nil {
				fmt.Fprintf(stderr, "cannot write reproduction bundle: %v\n", err)
				return 2
			}
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown protocol action %q (use seal or assess)\n", strings.TrimSpace(args[0]))
		return 2
	}
}

func protocolRoles(role string, count int) []string {
	roles := make([]string, count)
	for i := range roles {
		roles[i] = role
	}
	return roles
}

func protocolReplayCommand(protocol string, explorations, confirmations, failures []string, out, markdown, bundle string) string {
	args := []string{"damocline", "protocol", "assess", "--file", protocol}
	for _, group := range []struct {
		flag  string
		paths []string
	}{{"--exploration-campaign", explorations}, {"--confirmation-campaign", confirmations}, {"--failure-campaign", failures}} {
		for _, path := range group.paths {
			args = append(args, group.flag, path)
		}
	}
	for _, pair := range [][2]string{{"--out", out}, {"--markdown", markdown}, {"--bundle", bundle}} {
		if pair[1] != "" {
			args = append(args, pair[0], pair[1])
		}
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "''") + "'"
	}
	return strings.Join(quoted, " ")
}

func writeProtocolJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func ensureProtocolOutputDistinct(output string, inputs []string) error {
	absoluteOutput, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		absoluteInput, err := filepath.Abs(input)
		if err != nil {
			return err
		}
		if strings.EqualFold(filepath.Clean(absoluteOutput), filepath.Clean(absoluteInput)) {
			return fmt.Errorf("output path collides with input %q", input)
		}
	}
	return nil
}
