package page

import (
	"strings"
	"testing"
)

func TestPendingDraftsAreNamedAsASentence(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{[]string{"Ann"}, "Ann has a draft of this page"},
		{[]string{"Ann", "Ben"}, "Ann and Ben have drafts of this page"},
		{[]string{"Ann", "", "Cid"}, "Ann, Somebody and Cid have drafts of this page"},
	}
	for _, c := range cases {
		got := (&DraftsPendingError{Names: c.names}).Error()
		if !strings.HasPrefix(got, c.want) {
			t.Errorf("%v reads %q, want it to begin %q", c.names, got, c.want)
		}
		if !strings.Contains(got, "make it live anyway") {
			t.Errorf("%q does not say what to do", got)
		}
	}
}

func TestIssueKeysChangeOnlyWhenTheSetDoes(t *testing.T) {
	if !sameKeys([]string{"CP-1", "CP-2"}, []string{"CP-2", "CP-1"}) {
		t.Error("the same keys in another order count as a change")
	}
	if sameKeys([]string{"CP-1"}, []string{"CP-1", "CP-2"}) {
		t.Error("a key added does not count as a change")
	}
	if !sameKeys(nil, []string{}) {
		t.Error("no keys before and after count as a change")
	}
	keys := []string{"B-1", "A-1"}
	sameKeys(keys, nil)
	if keys[0] != "B-1" {
		t.Error("comparing sorted the caller's keys")
	}
}
