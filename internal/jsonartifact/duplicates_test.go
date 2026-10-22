package jsonartifact

import (
	"encoding/json"
	"testing"
)

func TestRejectDuplicateKeys(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{name: "distinct", json: `{"a":1,"b":2}`},
		{name: "nested objects are independent", json: `{"a":{"x":1},"b":{"x":2}}`},
		{name: "duplicate top level", json: `{"a":1,"a":2}`, wantErr: true},
		{name: "escaped duplicate", json: `{"schema_version":1,"schema_\u0076ersion":2}`, wantErr: true},
		{name: "duplicate nested", json: `{"outer":{"a":1,"a":2}}`, wantErr: true},
		{name: "duplicate in array object", json: `[{"a":1,"a":2}]`, wantErr: true},
		{name: "multiple values", json: `{} {}`, wantErr: true},
		{name: "malformed", json: `{"a":}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := RejectDuplicateKeys([]byte(test.json))
			if (err != nil) != test.wantErr {
				t.Fatalf("RejectDuplicateKeys() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func FuzzRejectDuplicateKeysNeverAcceptsInvalidJSON(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{}`),
		[]byte(`{"a":1,"a":2}`),
		[]byte(`{"a":{"b":[true,null,3]}}`),
		[]byte(`{"a":}`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if err := RejectDuplicateKeys(data); err == nil && !json.Valid(data) {
			t.Fatal("invalid JSON was accepted")
		}
	})
}
