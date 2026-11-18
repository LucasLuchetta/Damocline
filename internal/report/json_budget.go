package report

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var errReportTooLarge = errors.New("report exceeds the 16 MiB output limit")
var errReportResourceBudget = errors.New("report exceeds the serialization resource budget")

var timeType = reflect.TypeFor[time.Time]()
var jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
var textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
var zeroMethodType = reflect.TypeFor[interface{ IsZero() bool }]()

func reportTypeImplements(t, contract reflect.Type) bool {
	return t.Implements(contract) || (t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(contract))
}

func spendJSONString(budget *int, value string) error {
	if *budget < 2 || len(value) > *budget-2 {
		return errReportResourceBudget
	}
	*budget -= 2 // surrounding quotes
	for i := 0; i < len(value) && *budget >= 0; {
		b := value[i]
		if b < utf8.RuneSelf {
			switch {
			case b == '\\' || b == '"' || b == '\b' || b == '\f' || b == '\n' || b == '\r' || b == '\t':
				*budget -= 2
			case b < 0x20 || b == '<' || b == '>' || b == '&':
				*budget -= 6
			default:
				*budget--
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(value[i:])
		if (r == utf8.RuneError && size == 1) || r == '\u2028' || r == '\u2029' {
			*budget -= 6
		} else {
			*budget -= size
		}
		i += size
	}
	if *budget < 0 {
		return errReportResourceBudget
	}
	return nil
}

// checkReportShape bounds the data model inspected by encoding/json before it
// allocates complete compact and indented encodings. The 32 MiB resource budget
// includes escaped strings, structural work and indentation. The final encoded
// artifact still has its separate exact 16 MiB limit. This is a plain-data
// artifact writer: arbitrary serialization callbacks are not executed.
func checkReportShape(value any) error {
	budget := 2 * maxReportBytes
	var walk func(reflect.Value, int) error
	walk = func(v reflect.Value, depth int) error {
		budget -= 1 + 2*depth
		if budget < 0 {
			return errReportResourceBudget
		}
		if depth > 128 {
			return errors.New("report value nesting exceeds the safe limit")
		}
		if !v.IsValid() {
			budget -= 4
			if budget < 0 {
				return errReportResourceBudget
			}
			return nil
		}
		if v.Kind() == reflect.Interface {
			if v.IsNil() {
				budget -= 4
				if budget < 0 {
					return errReportResourceBudget
				}
				return nil
			}
			return walk(v.Elem(), depth+1)
		}
		if v.Type() == timeType || (v.Kind() == reflect.Pointer && v.Type().Elem() == timeType) {
			budget -= 64
			if budget < 0 {
				return errReportResourceBudget
			}
			return nil
		}
		if v.Type() == rawJSONType {
			return spendRawJSON(&budget, v.Bytes(), depth)
		}
		if v.Kind() == reflect.Pointer && v.Type().Elem() == rawJSONType {
			if v.IsNil() {
				budget -= 4
				if budget < 0 {
					return errReportResourceBudget
				}
				return nil
			}
			return walk(v.Elem(), depth+1)
		}
		if v.Type() == numberType {
			budget -= len(v.String())
			if budget < 0 {
				return errReportResourceBudget
			}
			return nil
		}
		if reportTypeImplements(v.Type(), jsonMarshalerType) || reportTypeImplements(v.Type(), textMarshalerType) {
			return errors.New("unsupported custom serialization method in report")
		}
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				return walk(v.Elem(), depth+1)
			}
			budget -= 4
		case reflect.String:
			return spendJSONString(&budget, v.String())
		case reflect.Slice, reflect.Array:
			if v.Kind() == reflect.Slice && v.IsNil() {
				budget -= 4
				break
			}
			if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
				if reportTypeImplements(v.Type().Elem(), jsonMarshalerType) || reportTypeImplements(v.Type().Elem(), textMarshalerType) {
					return errors.New("unsupported custom byte serialization in report")
				}
				if v.Len() > budget {
					return errReportResourceBudget
				}
				budget -= 4*((v.Len()+2)/3) + 2 // base64 JSON string
				break
			}
			if v.Len() > budget {
				return errReportResourceBudget
			}
			budget -= 2 // array delimiters
			if v.Len() > 0 {
				budget -= v.Len() + 2*depth
			} // commas and closing newline/indent
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i), depth+1); err != nil {
					return err
				}
			}
		case reflect.Map:
			keyType := v.Type().Key()
			knownTimeKey := keyType == timeType || (keyType.Kind() == reflect.Pointer && keyType.Elem() == timeType)
			if !knownTimeKey {
				if reportTypeImplements(keyType, textMarshalerType) || reportTypeImplements(keyType, jsonMarshalerType) {
					return errors.New("unsupported custom map key serialization in report")
				}
				switch keyType.Kind() {
				case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				default:
					return errors.New("unsupported map key type in report")
				}
			}
			if v.IsNil() {
				budget -= 4
				break
			}
			if v.Len() > budget {
				return errReportResourceBudget
			}
			budget -= 2
			if v.Len() > 0 {
				budget -= v.Len() + 2*depth
			}
			iter := v.MapRange()
			for iter.Next() {
				// MapIter.Key/Value copy their values. Charge that work before
				// either operation can allocate a large inline array or struct.
				copyBytes := v.Type().Key().Size() + v.Type().Elem().Size()
				if budget < 0 || copyBytes > uintptr(budget) {
					return errReportResourceBudget
				}
				budget -= int(copyBytes)
				key := iter.Key()
				if key.Kind() == reflect.String {
					// json.Number is numeric as a value, but escaped text as a key.
					budget -= 1 + 2*(depth+1)
					if err := spendJSONString(&budget, key.String()); err != nil {
						return err
					}
				} else {
					if err := walk(key, depth+1); err != nil {
						return err
					}
					budget -= 2 // quotes around a numeric or time key
				}
				if err := walk(iter.Value(), depth+1); err != nil {
					return err
				}
			}
		case reflect.Struct:
			budget -= 2
			fields := 0
			for i := 0; i < v.NumField(); i++ {
				field := v.Type().Field(i)
				if !field.Anonymous && field.PkgPath != "" {
					continue
				}
				// Tag lookup can scan/unquote metadata. Bound that work too.
				budget -= len(field.Tag)
				if budget < 0 {
					return errReportResourceBudget
				}
				name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
				if name == "-" {
					continue
				}
				// encoding/json caches escaped names before deciding whether a
				// field is empty or promoted. Inspect names before those skips.
				if err := spendReportFieldName(&budget, name, field.Name); err != nil {
					return err
				}
				budget -= 2 // colon and space (conservative for omitted fields)
				if field.Anonymous {
					if hasJSONOption(options, "omitzero") && reportTypeImplements(field.Type, zeroMethodType) {
						return errors.New("unsupported custom zero-value method in report")
					}
					// Inspect promoted fields too, including exported children of
					// an unexported embedded struct. Hidden conflicts may be charged
					// conservatively by this separate resource budget.
					if err := walk(v.Field(i), depth+1); err != nil {
						return err
					}
					fields++
					continue
				}
				fieldValue := v.Field(i)
				if hasJSONOption(options, "omitempty") && emptyJSONValue(fieldValue) {
					continue
				}
				if hasJSONOption(options, "omitzero") {
					knownTime := fieldValue.Type() == timeType || (fieldValue.Kind() == reflect.Pointer && fieldValue.Type().Elem() == timeType)
					if !knownTime && reportTypeImplements(fieldValue.Type(), zeroMethodType) {
						return errors.New("unsupported custom zero-value method in report")
					}
					if fieldValue.IsZero() {
						continue
					}
				}
				if err := walk(v.Field(i), depth+1); err != nil {
					return err
				}
				quotedValue := fieldValue
				for quotedValue.Kind() == reflect.Pointer && !quotedValue.IsNil() {
					quotedValue = quotedValue.Elem()
				}
				if hasJSONOption(options, "string") {
					switch quotedValue.Kind() {
					case reflect.String:
						// Double encoding a string can add another escape layer.
						if err := spendJSONString(&budget, quotedValue.String()); err != nil {
							return err
						}
					case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
						reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
						reflect.Float32, reflect.Float64:
						budget -= 2 // quotes around a string-encoded scalar
					}
				}
				fields++
			}
			if fields > 0 {
				budget -= fields + 2*depth
			}
		case reflect.Bool:
			budget -= 4
			if !v.Bool() {
				budget--
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			var scratch [32]byte
			budget -= len(strconv.AppendInt(scratch[:0], v.Int(), 10))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			var scratch [32]byte
			budget -= len(strconv.AppendUint(scratch[:0], v.Uint(), 10))
		case reflect.Float32, reflect.Float64:
			// Encode only a plain scalar: JSON's fixed/exponent choice can be
			// longer than strconv's general format (for example, 1e20).
			var scalar any = v.Float()
			if v.Kind() == reflect.Float32 {
				scalar = float32(v.Float())
			}
			encoded, err := json.Marshal(scalar)
			if err != nil {
				return fmt.Errorf("marshal report number: %w", err)
			}
			budget -= len(encoded)
		default:
			return errors.New("unsupported value type in report")
		}
		if budget < 0 {
			return errReportResourceBudget
		}
		return nil
	}
	return walk(reflect.ValueOf(value), 0)
}

var rawJSONType = reflect.TypeFor[json.RawMessage]()
var numberType = reflect.TypeFor[json.Number]()

// Invalid JSON tag names fall back to the Go field name. Charge the larger
// escaped name without duplicating encoding/json's field-selection algorithm.
func spendReportFieldName(budget *int, tagName, fieldName string) error {
	fieldBudget := *budget
	if err := spendJSONString(&fieldBudget, fieldName); err != nil {
		return err
	}
	if tagName == "" {
		*budget = fieldBudget
		return nil
	}
	tagBudget := *budget
	if err := spendJSONString(&tagBudget, tagName); err != nil {
		return err
	}
	*budget = min(fieldBudget, tagBudget)
	return nil
}

func hasJSONOption(options, want string) bool {
	for options != "" {
		option, rest, _ := strings.Cut(options, ",")
		if option == want {
			return true
		}
		options = rest
	}
	return false
}

func emptyJSONValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}

// Raw fragments are already serialized inputs. Bound their input bytes as well
// as HTML escaping and pretty-print work without invoking a custom marshaler.
func spendRawJSON(budget *int, raw []byte, depth int) error {
	if len(raw) > maxReportBytes {
		return errReportResourceBudget
	}
	if raw == nil {
		*budget -= 4
		if *budget < 0 {
			return errReportResourceBudget
		}
		return nil
	}
	if !json.Valid(raw) {
		return errors.New("invalid raw JSON in report")
	}
	quoted, escaped := false, false
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		*budget--
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			if b == '<' || b == '>' || b == '&' {
				*budget -= 5
			}
			if b == 0xe2 && i+2 < len(raw) && raw[i+1] == 0x80 && (raw[i+2] == 0xa8 || raw[i+2] == 0xa9) {
				*budget -= 3
			}
		} else {
			switch b {
			case '"':
				quoted = true
			case '{', '[':
				depth++
				*budget -= 1 + 2*depth
			case '}', ']':
				depth--
				*budget -= 1 + 2*depth
			case ',':
				*budget -= 1 + 2*depth
			case ':':
				*budget--
			}
			if depth > 128 {
				return errors.New("report value nesting exceeds the safe limit")
			}
		}
		if *budget < 0 {
			return errReportResourceBudget
		}
	}
	return nil
}
