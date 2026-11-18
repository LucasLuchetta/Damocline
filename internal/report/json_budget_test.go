package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type callbackReport struct{ Called *bool }

func (v *callbackReport) MarshalJSON() ([]byte, error) {
	*v.Called = true
	return []byte(`"callback"`), nil
}

type zeroCallbackReport struct{ Called *bool }

type textCallbackReport struct{ Called *bool }

func (v *textCallbackReport) MarshalText() ([]byte, error) {
	*v.Called = true
	return []byte("callback"), nil
}

func (v *zeroCallbackReport) IsZero() bool { *v.Called = true; return false }

type callbackReportByte byte

func (*callbackReportByte) MarshalJSON() ([]byte, error) { panic("byte callback executed") }

func TestReportBudgetRejectsCallbacksBeforeExecution(t *testing.T) {
	called := false
	for _, value := range []any{
		&callbackReport{Called: &called},
		&textCallbackReport{Called: &called},
		map[*textCallbackReport]string{&textCallbackReport{Called: &called}: "value"},
		&struct{ Value callbackReport }{Value: callbackReport{Called: &called}},
		&struct {
			Value zeroCallbackReport `json:"value,omitzero"`
		}{Value: zeroCallbackReport{Called: &called}},
		&struct {
			zeroCallbackReport `json:"value,omitzero"`
		}{zeroCallbackReport: zeroCallbackReport{Called: &called}},
		[]callbackReportByte{1},
	} {
		if err := WriteAnyJSON(filepath.Join(t.TempDir(), "report.json"), value); err == nil || called {
			t.Fatalf("serialization callback was accepted or executed: %T called=%t error=%v", value, called, err)
		}
	}
}

func TestReportBudgetPreservesStandardEncodingForArtifactValues(t *testing.T) {
	raw := json.RawMessage(`{"text":"<>&\"","nested":[true,null,{},[]]}`)
	at := time.Unix(1, 0).UTC()
	text := "<quoted>\n\""
	type hidden struct{ Payload string }
	for _, value := range []any{
		nil, json.RawMessage(nil), &raw, raw, []byte{0, 255}, [3]byte{0, 1, 255},
		&at, struct{ hidden }{hidden{Payload: "visible"}},
		struct{ Raw json.Marshaler }{Raw: raw}, struct{ Raw json.Marshaler }{},
		struct {
			Text *string `json:"text,string"`
		}{Text: &text},
		struct {
			Bool  bool    `json:"bool,string"`
			Int   int64   `json:"int,string"`
			Uint  uint64  `json:"uint,string"`
			Float float64 `json:"float,string"`
		}{Bool: true, Int: -123, Uint: 456, Float: 1e20},
		map[int]string{-10: "integer key"}, json.Number("123.456e-2"),
		map[time.Time]string{at: "time key"}, map[json.Number]int{json.Number("<key>"): 1},
		[]float64{0, 1e20, 1e-5, 1e-7, math.MaxFloat64}, []float32{1e20, 1e-5},
		struct {
			Text string `json:"text,string"`
		}{Text: "<quoted>\n\""},
		struct {
			At      time.Time      `json:"at"`
			Empty   string         `json:"empty,omitempty"`
			Zero    int            `json:"zero,omitzero"`
			Ignored callbackReport `json:"-"`
			Text    string         `json:"text"`
		}{At: time.Unix(1, 0).UTC(), Text: "\x00\xff\u2028\u2029<&>"},
	} {
		want, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "report.json")
		if err := WriteAnyJSON(path, value); err != nil {
			t.Fatalf("%T: %v", value, err)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, append(want, '\n')) {
			t.Fatalf("encoding changed for %T: error=%v", value, err)
		}
	}
}

func TestReportBudgetRejectsLargeStringWithoutEncodingAllocation(t *testing.T) {
	value := map[string]string{"payload": strings.Repeat("x", 64<<20)}
	path := filepath.Join(t.TempDir(), "absent", "report.json")
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := WriteAnyJSON(path, value)
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
		t.Fatalf("rejection allocated %d bytes; expected no full encoding buffer", allocated)
	}
	if !errors.Is(err, errReportResourceBudget) {
		t.Fatalf("large string error=%v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("rejection created output directory: %v", err)
	}
}

func TestReportBudgetRejectsLargeMapValueBeforeReflectionCopy(t *testing.T) {
	value := map[string][64 << 20]byte{"payload": {}}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := checkReportShape(value)
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
		t.Fatalf("resource inspection copied an oversized map value: allocated=%d", allocated)
	}
	if !errors.Is(err, errReportResourceBudget) {
		t.Fatalf("large map value error=%v", err)
	}
	runtime.KeepAlive(value)
}

func TestReportBudgetRejectsCyclesRawDepthAndInvalidJSON(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	deep := json.RawMessage(strings.Repeat("[", 129) + "0" + strings.Repeat("]", 129))
	for _, value := range []any{cycle, deep, json.RawMessage(`{"invalid":`), math.NaN(), func() {}, make(chan int), complex(1, 2), map[struct{}]int{{}: 1}} {
		path := filepath.Join(t.TempDir(), "report.json")
		if err := WriteAnyJSON(path, value); err == nil {
			t.Fatalf("invalid resource shape accepted: %T", value)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed value created output: %v", err)
		}
	}
}

func TestReportBudgetInspectsUnexportedEmbeddedField(t *testing.T) {
	type hidden struct{ Payload string }
	value := struct{ hidden }{hidden{Payload: strings.Repeat("x", 64<<20)}}
	if err := checkReportShape(value); !errors.Is(err, errReportResourceBudget) {
		t.Fatalf("unexported embedded fields bypassed resource inspection: %v", err)
	}
}

func FuzzReportJSONStringBudgetMatchesStandardLibrary(f *testing.F) {
	for _, value := range []string{"", "plain", "<>&\n\"", "\x00\xff\u2028\u2029"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 4096 {
			t.Skip()
		}
		budget := 1 << 16
		if err := spendJSONString(&budget, value); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil || (1<<16)-budget != len(encoded) {
			t.Fatalf("string budget differs from encoded length: charged=%d encoded=%d error=%v", (1<<16)-budget, len(encoded), err)
		}
	})
}

func FuzzReportRawJSONBudgetBoundsIndentation(f *testing.F) {
	for _, raw := range []string{`{}`, `[1,2]`, `{"key":"<>&"}`, `"\u2028"`, ` { "a": [true,null] } `} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4096 || !json.Valid(raw) {
			t.Skip()
		}
		budget := 2 * maxReportBytes
		if err := spendRawJSON(&budget, raw, 0); err != nil {
			return
		}
		encoded, err := json.MarshalIndent(json.RawMessage(raw), "", "  ")
		if err != nil || 2*maxReportBytes-budget < len(encoded) {
			t.Fatalf("raw budget underestimated indented output: charged=%d encoded=%d error=%v", 2*maxReportBytes-budget, len(encoded), err)
		}
	})
}

// ReportBudgetEmbedded is exported so reflection can construct a tagged embedded field.
type ReportBudgetEmbedded struct{ Value string }

func TestReportBudgetRejectsHugeFieldNamesBeforeEncodingCache(t *testing.T) {
	name := strings.Repeat("<", 6<<20)
	for _, tc := range []struct {
		label string
		field reflect.StructField
	}{
		{"tagged embedded field", reflect.StructField{Name: "ReportBudgetEmbedded", Type: reflect.TypeFor[ReportBudgetEmbedded](), Anonymous: true}},
		{"omitted empty field", reflect.StructField{Name: "Value", Type: reflect.TypeFor[string]()}},
	} {
		t.Run(tc.label, func(t *testing.T) {
			tc.field.Tag = reflect.StructTag(`json:"` + name + `,omitempty"`)
			model := reflect.New(reflect.StructOf([]reflect.StructField{tc.field})).Elem().Interface()
			if err := checkReportShape(model); !errors.Is(err, errReportResourceBudget) {
				t.Fatalf("oversized encoded field name escaped resource inspection: %v", err)
			}
			runtime.KeepAlive(model)
		})
	}
}

func TestReportBudgetCountsJSONNumberMapKeysAsStrings(t *testing.T) {
	value := map[json.Number]int{json.Number(strings.Repeat("<", 6<<20)): 1}
	if err := checkReportShape(value); !errors.Is(err, errReportResourceBudget) {
		t.Fatalf("escaped map key was charged as a numeric literal: %v", err)
	}
	runtime.KeepAlive(value)
}
