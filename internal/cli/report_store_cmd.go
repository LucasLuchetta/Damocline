package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/LucasLuchetta/Damocline/internal/reportstore"
)

func reportHistoryCmd(command string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("report "+command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", filepath.Join(".damocline", "reports"), "local report history directory")
	file := fs.String("file", "", "JSON artifact to copy into local report history")
	id := fs.String("id", "", "exact report history ID")
	keep := fs.Int("keep", 0, "number of newest reports to retain (1-100)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "positional arguments are not accepted")
		return 2
	}
	store := reportstore.Store{Dir: *dir}
	switch command {
	case "history":
		entries, err := store.List()
		if err != nil {
			fmt.Fprintf(stderr, "cannot read report history: %v\n", err)
			return 2
		}
		if len(entries) == 0 {
			fmt.Fprintln(stdout, "No archived reports.")
			return 0
		}
		fmt.Fprintln(stdout, "Saved at (UTC) | kind | scenario | outcome | bytes | ID")
		for _, entry := range entries {
			fmt.Fprintf(stdout, "%s | %s | %s | %s | %d | %s\n", entry.SavedAt.Format(time.RFC3339Nano), entry.Kind, safeHistoryField(entry.Scenario), safeHistoryField(entry.Outcome), entry.Bytes, entry.ID)
		}
		return 0
	case "save":
		if *file == "" {
			fmt.Fprintln(stderr, "usage: damocline report save --file artifact.json [--dir .damocline/reports]")
			return 2
		}
		entry, err := store.Save(*file)
		if err != nil {
			fmt.Fprintf(stderr, "cannot archive report: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Archived %s (%s, %d bytes)\n", entry.ID, entry.Kind, entry.Bytes)
		return 0
	case "delete":
		if *id == "" {
			fmt.Fprintln(stderr, "usage: damocline report delete --id <exact-history-id> [--dir .damocline/reports]")
			return 2
		}
		if err := store.Delete(*id); err != nil {
			fmt.Fprintf(stderr, "cannot delete report: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Deleted archived report %s\n", *id)
		return 0
	case "prune":
		if *keep == 0 {
			fmt.Fprintf(stderr, "usage: damocline report prune --keep 50 [--dir .damocline/reports] (1-%d; deletion is never automatic)\n", reportstore.MaxReports)
			return 2
		}
		removed, err := store.Prune(*keep)
		if err != nil {
			fmt.Fprintf(stderr, "cannot prune report history: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "Pruned %d older report(s); retained at most %d.\n", len(removed), *keep)
		return 0
	default:
		return 2
	}
}

func safeHistoryField(value string) string {
	var safe strings.Builder
	count := 0
	for _, char := range value {
		if count == 80 {
			safe.WriteString("…")
			break
		}
		if unicode.IsPrint(char) && !unicode.Is(unicode.Cf, char) {
			safe.WriteRune(char)
		} else {
			safe.WriteRune('?')
		}
		count++
	}
	return safe.String()
}
