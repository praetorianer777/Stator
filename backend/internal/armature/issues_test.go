package armature

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeKeyTakesArmaturesShapeOnly(t *testing.T) {
	for raw, want := range map[string]string{
		"CP-1":                   "CP-1",
		"cp-12":                  "CP-12",
		" Sec-2 ":                "SEC-2",
		"AB12-999":               "AB12-999",
		"ABCDEFGHIJ-1":           "ABCDEFGHIJ-1",
		"CP-123456789012345678":  "CP-123456789012345678",
		"X2-100000000000000000":  "X2-100000000000000000",
		"PROJ-9":                 "PROJ-9",
		"Q1-7":                   "Q1-7",
		"ZZ-1":                   "ZZ-1",
		"A1-1":                   "A1-1",
		"CP-10":                  "CP-10",
		"CP-99999999999999999":   "CP-99999999999999999",
		"ABCDEFGHIJ-12345678901": "ABCDEFGHIJ-12345678901",
	} {
		got, ok := NormalizeKey(raw)
		if !ok || got != want {
			t.Errorf("NormalizeKey(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "C-1", "1P-1", "CP-0", "CP-01", "CP1", "CP--1", "CP-1a", "ABCDEFGHIJK-1", "CP-1234567890123456789", "UTF-", "CP 1", "CP-1/../x", "ÄB-1"} {
		if got, ok := NormalizeKey(raw); ok {
			t.Errorf("NormalizeKey(%q) took it as %q", raw, got)
		}
	}
}

func TestIssueURLOpensTheIssueInArmature(t *testing.T) {
	for _, tc := range []struct{ base, key, want string }{
		{"https://armature.example.com", "CP-1", "https://armature.example.com/issues/CP-1"},
		{"http://localhost:8080/", "SEC-2", "http://localhost:8080/issues/SEC-2"},
	} {
		if got := IssueURL(tc.base, tc.key); got != tc.want {
			t.Errorf("IssueURL(%q, %q) = %q, want %q", tc.base, tc.key, got, tc.want)
		}
	}
}

// Armature's Issue answers dueDate as a date-time, where its inputs take a
// day; the block draws the day it names.
func TestAnIssueReadsArmaturesDueDate(t *testing.T) {
	var issue Issue
	if err := json.Unmarshal([]byte(`{"key":"CP-4","dueDate":"2026-10-15T00:00:00Z"}`), &issue); err != nil {
		t.Fatal(err)
	}
	if issue.DueDate == nil || !issue.DueDate.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("dueDate read as %v", issue.DueDate)
	}
	var none Issue
	if err := json.Unmarshal([]byte(`{"key":"CP-1"}`), &none); err != nil || none.DueDate != nil {
		t.Errorf("an issue without a due date reads %v, %v", none.DueDate, err)
	}
	out, _ := json.Marshal(none)
	if !strings.Contains(string(out), `"dueDate":null`) {
		t.Errorf("an issue without a due date is answered as %s", out)
	}
}
