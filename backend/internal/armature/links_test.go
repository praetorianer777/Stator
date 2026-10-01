package armature

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestKeysAreTheChipsAndIssueBlocksOfABody(t *testing.T) {
	body := json.RawMessage(`{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"See CP-9 and "},{"type":"armatureIssue","attrs":{"key":"cp-2"}}]},
		{"type":"armatureIssueBlock","attrs":{"key":"CP-1"}},
		{"type":"armatureIssueList","attrs":{"query":"key = CP-3","columns":["key"],"limit":20}},
		{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[
			{"type":"armatureIssue","attrs":{"key":"CP-2"}},{"type":"armatureIssue","attrs":{"key":"not a key"}}]}]}]},
		{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[
			{"type":"armatureIssue","attrs":{"key":"SEC-1"}}]}]}]}]}
	]}`)
	if got, want := KeysIn(body), []string{"CP-2", "CP-1", "SEC-1"}; !slices.Equal(got, want) {
		t.Errorf("the keys are %v, want %v: bare text and list queries name nothing, a key named twice is listed once", got, want)
	}
	for _, empty := range []string{``, `null`, `{"type":"doc"}`, `not json`} {
		if got := KeysIn(json.RawMessage(empty)); len(got) != 0 {
			t.Errorf("%q names %v", empty, got)
		}
	}
}

func TestARestrictedPageIsNotNamedOnAnIssue(t *testing.T) {
	if got := LinkTitle("Payroll for 2027", false); got != RestrictedLinkTitle {
		t.Errorf("a restricted page is titled %q", got)
	}
	if got := LinkTitle("Runbook", true); got != "Runbook" {
		t.Errorf("an open page is titled %q", got)
	}
	if got := LinkTitle(strings.Repeat("ä", MaxLinkTitle+5), true); len([]rune(got)) != MaxLinkTitle {
		t.Errorf("a long title keeps %d characters", len([]rune(got)))
	}
}

func TestPlanningComparesWhatIsWantedWithWhatWasRecorded(t *testing.T) {
	id := func() *uuid.UUID { v := uuid.New(); return &v }
	page := WantedLink{URL: "https://wiki.example/s/ENG/p/1", Title: "Runbook"}
	have := []LinkRecord{
		{Key: "CP-1", RemoteID: id(), URL: page.URL, Title: page.Title, State: LinkSynced},
		{Key: "CP-2", RemoteID: id(), URL: page.URL, Title: "Old title", State: LinkSynced},
		{Key: "CP-3", RemoteID: id(), URL: "https://wiki.example/s/OPS/p/1", Title: page.Title, State: LinkSynced},
		{Key: "CP-4", URL: page.URL, Title: page.Title, State: LinkFailed},
		{Key: "CP-5", URL: page.URL, Title: page.Title, State: LinkPending},
		{Key: "CP-6", RemoteID: id(), URL: page.URL, Title: page.Title, State: LinkSynced},
		{Key: "CP-7", State: LinkFailed},
	}
	want := map[string]WantedLink{"CP-1": page, "CP-2": page, "CP-3": page, "CP-4": page, "CP-5": page, "CP-8": page}

	var puts, removes []string
	for _, step := range PlanLinks(want, have) {
		if step.Want != nil {
			puts = append(puts, step.Key)
			if step.Key == "CP-3" && (step.Have == nil || step.Have.URL == page.URL) {
				t.Errorf("a link whose page moved is put without the old address to take off: %+v", step.Have)
			}
			continue
		}
		removes = append(removes, step.Key)
	}
	if want := []string{"CP-2", "CP-3", "CP-4", "CP-5", "CP-8"}; !slices.Equal(puts, want) {
		t.Errorf("the links put are %v, want %v: synced and unchanged ones are left alone, failed and pending ones tried again", puts, want)
	}
	if want := []string{"CP-6", "CP-7"}; !slices.Equal(removes, want) {
		t.Errorf("the links taken off are %v, want %v", removes, want)
	}
	if steps := PlanLinks(nil, nil); len(steps) != 0 {
		t.Errorf("nothing wanted and nothing held plans %v", steps)
	}
	if steps := PlanLinks(map[string]WantedLink{"CP-1": page}, have[:1]); len(steps) != 0 {
		t.Errorf("a second sync of the same page plans %v", steps)
	}
}
