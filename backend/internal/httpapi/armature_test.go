package httpapi

import (
	"fmt"
	"slices"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

func TestLookupKeysAreDistinctUpperCaseAndBounded(t *testing.T) {
	keys, err := lookupKeys([]string{"cp-1", "SEC-2", "CP-1", " cp-3 "})
	if err != nil || !slices.Equal(keys, []string{"CP-1", "SEC-2", "CP-3"}) {
		t.Errorf("lookupKeys = %v, %v", keys, err)
	}

	many := make([]string, 0, armature.MaxLookupKeys+1)
	for i := 1; i <= armature.MaxLookupKeys; i++ {
		many = append(many, fmt.Sprintf("CP-%d", i))
	}
	if _, err := lookupKeys(many); err != nil {
		t.Errorf("%d keys were refused: %v", len(many), err)
	}
	if _, err := lookupKeys(append(many, "CP-1")); err != nil {
		t.Errorf("a key asked twice counted twice: %v", err)
	}
	for name, raw := range map[string][]string{
		"none":         nil,
		"too many":     append(slices.Clone(many), "SEC-1"),
		"not a key":    {"CP-1", "UTF-8x"},
		"a whole link": {"https://armature.example.com/issues/CP-1"},
	} {
		_, err := lookupKeys(raw)
		var apiErr *APIError
		if e, ok := err.(*APIError); ok {
			apiErr = e
		}
		if apiErr == nil || apiErr.Status != 422 || apiErr.Fields["key"] == "" {
			t.Errorf("%s: %v, want 422 on key", name, err)
		}
	}
}
