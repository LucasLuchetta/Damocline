package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// DataSource references a local CSV file whose rows feed ${data.column}
// placeholders in step paths, header values, and environment-backed bodies.
type DataSource struct {
	File string `json:"file"`
	// Mode is sequential (default), random, or unique_per_vu.
	Mode string `json:"mode,omitempty"`
}

// Data modes.
const (
	DataModeSequential  = "sequential"
	DataModeRandom      = "random"
	DataModeUniquePerVU = "unique_per_vu"
)

const (
	maxDataBytes       = 4 << 20
	maxDataRows        = 100000
	maxDataColumns     = 50
	maxDataValueBytes  = 4096
	dataVariablePrefix = "data_"
)

var dataColumnPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,58}$`)
var dataPlaceholderPattern = regexp.MustCompile(`\$\{data\.([^}]*)\}`)

// DataSet is a validated, immutable CSV table loaded at scenario validation.
type DataSet struct {
	Mode    string
	Columns []string
	Rows    [][]string
	SHA256  string
}

// Select returns the row for one journey. sequence is a run-wide journey
// counter starting at 1; actor is the zero-based virtual-user index.
func (d *DataSet) Select(actor int, sequence, seed uint64) []string {
	n := uint64(len(d.Rows))
	switch d.Mode {
	case DataModeUniquePerVU:
		return d.Rows[uint64(actor)%n]
	case DataModeRandom:
		return d.Rows[splitMix64(seed^(sequence*0x9e3779b97f4a7c15))%n]
	default:
		return d.Rows[(sequence-1)%n]
	}
}

// Variables returns the template variables for one selected row.
func (d *DataSet) Variables(row []string, into map[string]string) {
	for i, column := range d.Columns {
		into[dataVariablePrefix+column] = row[i]
	}
}

func splitMix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func loadDataSet(source *DataSource, users int) (*DataSet, error) {
	if source == nil {
		return nil, nil
	}
	mode := source.Mode
	if mode == "" {
		mode = DataModeSequential
	}
	if mode != DataModeSequential && mode != DataModeRandom && mode != DataModeUniquePerVU {
		return nil, errors.New("data.mode must be sequential, random, or unique_per_vu")
	}
	if strings.TrimSpace(source.File) == "" || strings.ContainsAny(source.File, "\r\n\x00") {
		return nil, errors.New("data.file must name a local CSV file")
	}
	f, err := os.Open(source.File)
	if err != nil {
		return nil, errors.New("cannot open data.file")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxDataBytes+1))
	if err != nil {
		return nil, errors.New("cannot read data.file")
	}
	if len(raw) > maxDataBytes {
		return nil, fmt.Errorf("data.file exceeds the %d-byte limit", maxDataBytes)
	}
	digest := sha256.Sum256(raw)
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("data.file must start with a CSV header row")
	}
	if len(header) > maxDataColumns {
		return nil, fmt.Errorf("data.file supports at most %d columns", maxDataColumns)
	}
	seen := make(map[string]bool, len(header))
	for _, column := range header {
		if !dataColumnPattern.MatchString(column) || seen[column] {
			return nil, fmt.Errorf("data.file column %q must be unique and match [a-z][a-z0-9_]{0,58}", column)
		}
		seen[column] = true
	}
	set := &DataSet{Mode: mode, Columns: header, SHA256: "sha256:" + hex.EncodeToString(digest[:])}
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// The CSV error names a line number but never a cell value.
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				return nil, fmt.Errorf("data.file is invalid CSV at line %d", parseErr.Line)
			}
			return nil, errors.New("data.file is invalid CSV")
		}
		for _, value := range record {
			if len(value) > maxDataValueBytes || strings.ContainsAny(value, "\r\n\x00") {
				return nil, fmt.Errorf("data.file row %d has a value over %d bytes or with a line break", len(set.Rows)+1, maxDataValueBytes)
			}
		}
		set.Rows = append(set.Rows, record)
		if len(set.Rows) > maxDataRows {
			return nil, fmt.Errorf("data.file supports at most %d rows", maxDataRows)
		}
	}
	if len(set.Rows) == 0 {
		return nil, errors.New("data.file must contain at least one data row")
	}
	if mode == DataModeUniquePerVU && len(set.Rows) < users {
		return nil, fmt.Errorf("data.mode unique_per_vu needs at least %d rows (one per virtual user); found %d", users, len(set.Rows))
	}
	return set, nil
}

// rewriteDataPlaceholders converts ${data.column} into the internal
// {{data_column}} template form after checking that the column exists.
func rewriteDataPlaceholders(value string, data *DataSet) (string, error) {
	if !strings.Contains(value, "${data.") {
		return value, nil
	}
	if data == nil {
		return "", errors.New("${data.*} placeholders require a data source")
	}
	var failure error
	rewritten := dataPlaceholderPattern.ReplaceAllStringFunc(value, func(match string) string {
		column := dataPlaceholderPattern.FindStringSubmatch(match)[1]
		for _, known := range data.Columns {
			if known == column {
				return "{{" + dataVariablePrefix + column + "}}"
			}
		}
		if failure == nil {
			failure = fmt.Errorf("placeholder ${data.%s} names no column in data.file", column)
		}
		return match
	})
	if failure != nil {
		return "", failure
	}
	if strings.Contains(rewritten, "${data.") {
		return "", errors.New("malformed ${data.*} placeholder")
	}
	return rewritten, nil
}
