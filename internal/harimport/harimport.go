// Package harimport converts a browser HAR capture into a reviewable
// Damocline scenario draft. It never copies credentials, cookies, tokens, or
// request bodies: they become ${ENV} placeholders that the operator supplies
// locally at run time (ADR 0178).
package harimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/LucasLuchetta/Damocline/internal/config"
)

// MaxSteps mirrors the scenario-wide step ceiling enforced by config.
const MaxSteps = 20

// Options controls which HAR entries become draft steps.
type Options struct {
	// Name is the scenario name written into the draft.
	Name string
	// KeepStatic retains static assets (scripts, styles, images, fonts).
	KeepStatic bool
	// IncludeWrites retains POST, PUT, PATCH, and DELETE requests. Running the
	// draft still requires --allow-writes.
	IncludeWrites bool
}

// Dropped records one HAR entry that was not converted and why. Target is a
// host (cross-origin) or a path without query; it never holds query values.
type Dropped struct {
	Method string `json:"method"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// Result is the draft plus the review evidence the operator must inspect.
type Result struct {
	Scenario     config.Scenario
	Dropped      []Dropped
	Placeholders []string
	WriteSteps   []string
	RemovedQuery []string
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harEntry struct {
	PageRef string `json:"pageref"`
	Request struct {
		Method   string         `json:"method"`
		URL      string         `json:"url"`
		Headers  []harNameValue `json:"headers"`
		Cookies  []harNameValue `json:"cookies"`
		PostData *struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status  int `json:"status"`
		Content struct {
			MimeType string `json:"mimeType"`
		} `json:"content"`
	} `json:"response"`
}

type harDocument struct {
	Log struct {
		Pages []struct {
			ID string `json:"id"`
		} `json:"pages"`
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

var staticExtensions = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".map": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".svg": true, ".ico": true, ".webp": true, ".avif": true, ".bmp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".mp4": true, ".webm": true, ".mp3": true, ".wav": true, ".ogg": true,
}

var (
	sensitiveQueryKey = regexp.MustCompile(`(?i)token|secret|password|passwd|credential|session|auth|signature|^sig$|api[-_]?key|apikey|csrf|xsrf|nonce|otp|^code$|^state$|magic`)
	sensitiveHeader   = regexp.MustCompile(`(?i)token|secret|session|auth|signature|api[-_]?key|apikey|csrf|xsrf`)
	sensitiveSegment  = regexp.MustCompile(`(?i)token|password|secret|credential|reset|oauth|signature|api-key|magic-link`)
	tokenLike         = regexp.MustCompile(`^[A-Za-z0-9_\-.~+/=%]{32,}$`)
	slugUnsafe        = regexp.MustCompile(`[^a-z0-9]+`)
	envUnsafe         = regexp.MustCompile(`[^A-Z0-9]+`)
)

// keptHeaders are copied literally because they describe content negotiation,
// not identity.
var keptHeaders = map[string]bool{"accept": true, "content-type": true, "x-requested-with": true}

// Convert parses HAR bytes and returns a scenario draft for base.
func Convert(data []byte, base *url.URL, o Options) (Result, error) {
	var doc harDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return Result{}, errors.New("invalid HAR input")
	}
	if base == nil || base.Host == "" {
		return Result{}, errors.New("an explicit base URL is required")
	}
	name := o.Name
	if name == "" {
		name = "har-import-draft"
	}
	pageIndex := map[string]int{}
	for i, page := range doc.Log.Pages {
		pageIndex[page.ID] = i + 1
	}
	res := Result{}
	placeholders := map[string]bool{}
	removed := map[string]bool{}
	steps := make([]config.Step, 0, MaxSteps)
	usedNames := map[string]bool{}
	for _, entry := range doc.Log.Entries {
		method := strings.ToUpper(strings.TrimSpace(entry.Request.Method))
		u, err := url.Parse(entry.Request.URL)
		if err != nil || u.Host == "" {
			res.Dropped = append(res.Dropped, Dropped{Method: method, Target: "(unparseable URL)", Reason: "invalid_url"})
			continue
		}
		target := u.EscapedPath()
		if target == "" {
			target = "/"
		}
		drop := func(reason string) {
			res.Dropped = append(res.Dropped, Dropped{Method: method, Target: target, Reason: reason})
		}
		if u.User != nil || !strings.EqualFold(u.Scheme, base.Scheme) || !strings.EqualFold(u.Host, base.Host) {
			target = u.Hostname()
			drop("cross_origin")
			continue
		}
		write := method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
		if method != "GET" && method != "HEAD" && !write {
			drop("unsupported_method")
			continue
		}
		if !o.KeepStatic && isStatic(u.Path, entry.Response.Content.MimeType) {
			drop("static_asset")
			continue
		}
		if write && !o.IncludeWrites {
			drop("write_method_not_opted_in")
			continue
		}
		if sensitivePath(u.Path) {
			target = "(redacted path)"
			drop("sensitive_path")
			continue
		}
		if len(steps) == MaxSteps {
			drop("step_limit")
			continue
		}
		stepPath := target
		if u.RawQuery != "" {
			query, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				drop("invalid_query")
				continue
			}
			for key, values := range query {
				sensitive := sensitiveQueryKey.MatchString(key) || strings.ContainsAny(key, "{}$")
				for _, value := range values {
					sensitive = sensitive || tokenLike.MatchString(value) || strings.ContainsAny(value, "{}$")
				}
				if sensitive {
					query.Del(key)
					removed[key] = true
				}
			}
			if encoded := query.Encode(); encoded != "" {
				stepPath += "?" + encoded
			}
		}
		page := pageIndex[entry.PageRef]
		stepName := uniqueName(fmt.Sprintf("p%02d-%02d-%s-%s", page, len(steps)+1, strings.ToLower(method), slug(u.Path)), usedNames)
		step := config.Step{Name: stepName, Method: method, Path: stepPath}
		if status := entry.Response.Status; status >= 100 && status <= 599 {
			step.ExpectedStatus = []int{status}
		}
		headers := map[string]string{}
		for _, header := range entry.Request.Headers {
			lower := strings.ToLower(strings.TrimSpace(header.Name))
			switch {
			case lower == "" || strings.HasPrefix(lower, ":"):
			case lower == "cookie":
				headers["Cookie"] = placeholder("DAMOCLINE_COOKIE", placeholders)
			case lower == "authorization" || lower == "proxy-authorization":
				headers[header.Name] = placeholder("DAMOCLINE_"+envName(lower), placeholders)
			case sensitiveHeader.MatchString(lower) || strings.HasPrefix(lower, "x-api"):
				headers[header.Name] = placeholder("DAMOCLINE_HEADER_"+envName(lower), placeholders)
			case keptHeaders[lower] && !strings.ContainsAny(header.Value, "\r\n{}$"):
				headers[header.Name] = header.Value
			}
		}
		if len(entry.Request.Cookies) > 0 {
			headers["Cookie"] = placeholder("DAMOCLINE_COOKIE", placeholders)
		}
		if len(headers) > 0 {
			step.Headers = headers
		}
		if entry.Request.PostData != nil && entry.Request.PostData.Text != "" && (method == "POST" || method == "PUT" || method == "PATCH") {
			// Bodies can carry credentials or personal data; only a named
			// placeholder is written and the operator supplies the content.
			step.BodyEnv = fmt.Sprintf("DAMOCLINE_BODY_%02d", len(steps)+1)
			placeholders[step.BodyEnv] = true
		}
		if write {
			res.WriteSteps = append(res.WriteSteps, stepName)
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return res, errors.New("HAR contains no convertible same-origin requests")
	}
	res.Scenario = config.Scenario{
		ScenarioVersion: config.CurrentScenarioVersion,
		Name:            name,
		Environment:     "draft",
		BaseURL:         base.Scheme + "://" + base.Host,
		Users:           1,
		Duration:        "30s",
		RampUp:          "0s",
		ThinkTime:       "1s",
		Timeout:         "8s",
		MaxRPS:          5,
		P95LimitMS:      1000,
		MaxErrorRate:    0.01,
		Workflows:       []config.Profile{{Name: "imported", Weight: 1, Steps: steps}},
	}
	for name := range placeholders {
		res.Placeholders = append(res.Placeholders, name)
	}
	sort.Strings(res.Placeholders)
	for key := range removed {
		res.RemovedQuery = append(res.RemovedQuery, key)
	}
	sort.Strings(res.RemovedQuery)
	return res, nil
}

func placeholder(name string, used map[string]bool) string {
	used[name] = true
	return "${" + name + "}"
}

func envName(header string) string {
	return strings.Trim(envUnsafe.ReplaceAllString(strings.ToUpper(header), "_"), "_")
}

func slug(p string) string {
	s := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(p), "-"), "-")
	if s == "" {
		s = "root"
	}
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

func uniqueName(name string, used map[string]bool) string {
	candidate := name
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s-%d", name, i)
	}
	used[candidate] = true
	return candidate
}

func isStatic(p, mime string) bool {
	if staticExtensions[strings.ToLower(path.Ext(p))] {
		return true
	}
	mime = strings.ToLower(mime)
	for _, prefix := range []string{"image/", "font/", "video/", "audio/", "text/css", "text/javascript", "application/javascript", "application/x-javascript", "application/font"} {
		if strings.HasPrefix(mime, prefix) {
			return true
		}
	}
	return false
}

func sensitivePath(p string) bool {
	for _, segment := range strings.Split(p, "/") {
		if segment == "" {
			continue
		}
		if tokenLike.MatchString(segment) || (strings.Count(segment, ".") >= 2 && len(segment) >= 24) ||
			sensitiveSegment.MatchString(segment) || strings.ContainsAny(segment, "{}$") || strings.Contains(segment, "..") {
			return true
		}
	}
	return false
}
