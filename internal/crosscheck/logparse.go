// Package crosscheck compares Damocline run counters with independent,
// server-side evidence such as an access log. It is offline: it reads files
// only and never contacts a target.
package crosscheck

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Log formats accepted by ParseLog.
const (
	FormatNginx    = "nginx"
	FormatCombined = "combined"
	FormatJSON     = "json"
)

// Entry is one server-observed request. DurationMS is negative when the log
// line carries no server-side duration.
type Entry struct {
	Time       time.Time
	Method     string
	Path       string
	Status     int
	DurationMS float64
}

// ParseResult holds parsed entries and the number of lines that could not be
// parsed. Unparsed lines are counted, never silently dropped.
type ParseResult struct {
	Entries  []Entry
	Lines    int
	Unparsed int
}

const maxLogLine = 1 << 20

// ParseLog reads an access log in the named format.
//
//   - combined: Apache/nginx combined log format. An optional trailing numeric
//     field is not interpreted.
//   - nginx: combined format followed by a trailing $request_time field in
//     seconds (for example `... "agent" 0.012`). Lines without it are accepted
//     with no duration.
//   - json: one JSON object per line with keys time (RFC 3339 or Unix
//     seconds), method, path (or uri/request_uri), status, and optionally
//     duration_ms or request_time (seconds).
func ParseLog(r io.Reader, format string) (ParseResult, error) {
	var parse func(string) (Entry, bool)
	switch format {
	case FormatNginx:
		parse = func(l string) (Entry, bool) { return parseCombined(l, true) }
	case FormatCombined:
		parse = func(l string) (Entry, bool) { return parseCombined(l, false) }
	case FormatJSON:
		parse = parseJSONLine
	default:
		return ParseResult{}, fmt.Errorf("unsupported log format %q (want nginx, combined or json)", format)
	}
	var out ParseResult
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLogLine)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		out.Lines++
		e, ok := parse(line)
		if !ok {
			out.Unparsed++
			continue
		}
		out.Entries = append(out.Entries, e)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("read access log: %w", err)
	}
	return out, nil
}

const clfTime = "02/Jan/2006:15:04:05 -0700"

// parseCombined parses `host ident user [time] "METHOD path PROTO" status size ...`.
func parseCombined(line string, trailingDuration bool) (Entry, bool) {
	open := strings.IndexByte(line, '[')
	closeIdx := strings.IndexByte(line, ']')
	if open < 0 || closeIdx < open {
		return Entry{}, false
	}
	ts, err := time.Parse(clfTime, line[open+1:closeIdx])
	if err != nil {
		return Entry{}, false
	}
	rest := line[closeIdx+1:]
	q1 := strings.IndexByte(rest, '"')
	if q1 < 0 {
		return Entry{}, false
	}
	q2 := strings.IndexByte(rest[q1+1:], '"')
	if q2 < 0 {
		return Entry{}, false
	}
	req := strings.Fields(rest[q1+1 : q1+1+q2])
	if len(req) < 2 {
		return Entry{}, false
	}
	after := strings.Fields(rest[q1+1+q2+1:])
	if len(after) < 1 {
		return Entry{}, false
	}
	status, err := strconv.Atoi(after[0])
	if err != nil || status < 100 || status > 999 {
		return Entry{}, false
	}
	e := Entry{Time: ts, Method: strings.ToUpper(req[0]), Path: req[1], Status: status, DurationMS: -1}
	if trailingDuration {
		last := line[strings.LastIndexByte(line, ' ')+1:]
		if !strings.HasSuffix(last, "\"") {
			if sec, err := strconv.ParseFloat(last, 64); err == nil && sec >= 0 {
				e.DurationMS = sec * 1000
			}
		}
	}
	return e, true
}

func parseJSONLine(line string) (Entry, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Entry{}, false
	}
	e := Entry{DurationMS: -1}
	switch v := raw["time"].(type) {
	case string:
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			if t, err = time.Parse(clfTime, v); err != nil {
				return Entry{}, false
			}
		}
		e.Time = t
	case float64:
		sec := int64(v)
		e.Time = time.Unix(sec, int64((v-float64(sec))*1e9)).UTC()
	default:
		return Entry{}, false
	}
	e.Method, _ = raw["method"].(string)
	e.Method = strings.ToUpper(e.Method)
	for _, k := range []string{"path", "uri", "request_uri"} {
		if s, ok := raw[k].(string); ok && s != "" {
			e.Path = s
			break
		}
	}
	switch v := raw["status"].(type) {
	case float64:
		e.Status = int(v)
	case string:
		e.Status, _ = strconv.Atoi(v)
	}
	if e.Method == "" || e.Path == "" || e.Status < 100 || e.Status > 999 {
		return Entry{}, false
	}
	if d, ok := number(raw["duration_ms"]); ok {
		e.DurationMS = d
	} else if d, ok := number(raw["request_time"]); ok {
		e.DurationMS = d * 1000
	}
	return e, true
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, n >= 0
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil && f >= 0
	}
	return 0, false
}
