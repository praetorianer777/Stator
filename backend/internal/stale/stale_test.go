package stale

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAnEmptyQueryIsEverySpaceAfterTheDefaultPeriod(t *testing.T) {
	f, problems := ParseFilter(url.Values{})
	if len(problems) != 0 {
		t.Fatalf("an empty query has problems: %v", problems)
	}
	if f.SpaceKey != "" || f.Owner != nil || f.Unowned || f.Verification != "" || f.Days != DefaultDays {
		t.Errorf("an empty query reads %+v", f)
	}
}

func TestTheFilterReadsEachParameter(t *testing.T) {
	owner := uuid.New()
	f, problems := ParseFilter(url.Values{
		"space": {" ops "}, "owner": {owner.String()}, "verification": {"expired"}, "olderThan": {"30"},
	})
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if f.SpaceKey != "ops" || f.Owner == nil || *f.Owner != owner || f.Unowned || f.Verification != Expired || f.Days != 30 {
		t.Errorf("the filter reads %+v", f)
	}
	if f.Archived {
		t.Errorf("archived pages are listed unasked")
	}
	if f, _ := ParseFilter(url.Values{"archived": {"true"}}); !f.Archived {
		t.Errorf("archived=true leaves archived pages out")
	}
	if f, problems := ParseFilter(url.Values{"archived": {"false"}}); f.Archived || len(problems) != 0 {
		t.Errorf("archived=false reads %+v, %v", f, problems)
	}
	f, _ = ParseFilter(url.Values{"owner": {Unowned}, "verification": {"none"}})
	if !f.Unowned || f.Owner != nil || f.Verification != Unverified {
		t.Errorf("pages without an owner or a verification read %+v", f)
	}
}

func TestWhatTheFilterCannotTakeIsASentenceForItsParameter(t *testing.T) {
	for _, tt := range []struct {
		param, value string
	}{
		{"owner", "somebody"},
		{"verification", "checked"},
		{"olderThan", "soon"},
		{"olderThan", strconv.Itoa(MinDays - 1)},
		{"olderThan", strconv.Itoa(MaxDays + 1)},
		{"olderThan", "1.5"},
		{"archived", "yes"},
	} {
		_, problems := ParseFilter(url.Values{tt.param: {tt.value}})
		msg, ok := problems[tt.param]
		if !ok || len(problems) != 1 {
			t.Errorf("%s=%s: problems %v", tt.param, tt.value, problems)
			continue
		}
		if !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] {
			t.Errorf("%s=%s is refused with %q, not a sentence", tt.param, tt.value, msg)
		}
	}
	for _, days := range []int{MinDays, MaxDays} {
		if f, problems := ParseFilter(url.Values{"olderThan": {strconv.Itoa(days)}}); len(problems) != 0 || f.Days != days {
			t.Errorf("olderThan=%d: %+v, %v", days, f, problems)
		}
	}
}

func TestEveryVerificationIsOneTheFilterTakes(t *testing.T) {
	for _, v := range Verifications {
		if f, problems := ParseFilter(url.Values{"verification": {string(v)}}); len(problems) != 0 || f.Verification != v {
			t.Errorf("%s: %+v, %v", v, f, problems)
		}
	}
}
