package search

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSplitMarksMatchesAsPlainRuns(t *testing.T) {
	cases := []struct {
		name   string
		marked string
		want   []Segment
	}{
		{"nothing matched", "just words", []Segment{{"just words", false}}},
		{"one match", "the " + startSel + "plan" + stopSel + " for", []Segment{{"the ", false}, {"plan", true}, {" for", false}}},
		{"a phrase is one match", startSel + "quick" + stopSel + " " + startSel + "brown" + stopSel + " fox", []Segment{{"quick brown", true}, {" fox", false}}},
		{"words apart stay apart", startSel + "a" + stopSel + " b " + startSel + "c" + stopSel, []Segment{{"a", true}, {" b ", false}, {"c", true}}},
		{"whitespace collapses", "  line one\n\n\tline " + startSel + "two" + stopSel + "  ", []Segment{{"line one line ", false}, {"two", true}}},
		{"stray delimiters drop", stopSel + "x" + startSel + startSel + "y" + stopSel + stopSel, []Segment{{"x", false}, {"y", true}}},
		{"markup stays text", openAngle + "b>" + startSel + "&amp;" + stopSel, []Segment{{"<b>", false}, {"&amp;", true}}},
		{"empty", "", []Segment{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Split(c.marked); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Split(%q) = %#v, want %#v", c.marked, got, c.want)
			}
		})
	}
}

func TestPlainNeverMatches(t *testing.T) {
	got := Plain("a " + startSel + "b" + stopSel)
	if !reflect.DeepEqual(got, []Segment{{"a b", false}}) {
		t.Fatalf("Plain = %#v", got)
	}
	if got := firstWords("one  two\nthree four", 3); got != "one two three" {
		t.Fatalf("firstWords = %q", got)
	}
}

func TestParseQueryReadsEveryFilter(t *testing.T) {
	v := url.Values{
		"q":             {"  deploy \"blue green\" -old "},
		"space":         {"ops", " Dev ", ""},
		"author":        {"0199a5e0-0000-7000-8000-000000000001"},
		"label":         {"runbook"},
		"type":          {"page", "comment"},
		"updatedAfter":  {"2026-09-01"},
		"updatedBefore": {"2026-10-01"},
	}
	q, err := ParseQuery(v)
	if err != nil {
		t.Fatal(err)
	}
	if q.Text != `deploy "blue green" -old` || !reflect.DeepEqual(q.Spaces, []string{"OPS", "DEV"}) ||
		len(q.Authors) != 1 || !reflect.DeepEqual(q.Labels, []string{"runbook"}) ||
		!reflect.DeepEqual(q.Types, []HitType{HitPage, HitComment}) || q.ByUpdate {
		t.Fatalf("parsed %+v", q)
	}
	if !q.After.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || !q.Before.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("days %v %v", q.After, q.Before)
	}
	if !q.wants(HitPage) || q.wants(HitAttachment) {
		t.Fatal("types are not honoured")
	}
}

func TestParseQueryOrders(t *testing.T) {
	for _, c := range []struct {
		values   url.Values
		byUpdate bool
	}{
		{url.Values{"q": {"x"}}, false},
		{url.Values{}, true},
		{url.Values{"q": {"x"}, "sort": {"updated"}}, true},
		{url.Values{"sort": {"relevance"}}, true},
	} {
		q, err := ParseQuery(c.values)
		if err != nil || q.ByUpdate != c.byUpdate {
			t.Errorf("%v: by update %v (%v), want %v", c.values, q.ByUpdate, err, c.byUpdate)
		}
	}
}

func TestParseQueryRefusesBadInput(t *testing.T) {
	for field, v := range map[string]url.Values{
		"q":             {"q": {strings.Repeat("x", MaxQueryLength+1)}},
		"author":        {"author": {"alice"}},
		"type":          {"type": {"file"}},
		"updatedAfter":  {"updatedAfter": {"2026-13-01"}},
		"updatedBefore": {"updatedBefore": {"yesterday"}},
		"sort":          {"sort": {"title"}},
		"asTyped":       {"asTyped": {"yes"}},
	} {
		_, err := ParseQuery(v)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != field || !strings.HasSuffix(fe.Message, ".") {
			t.Errorf("%v: %v, want a sentence on %s", v, err, field)
		}
	}
	if _, err := ParseQuery(url.Values{"q": {strings.Repeat("é", MaxQueryLength)}}); err != nil {
		t.Errorf("200 characters are refused: %v", err)
	}
}

func TestPrefixQueryKeepsOnlyWords(t *testing.T) {
	for typed, want := range map[string]string{
		"":                     "",
		"  !!  ":               "",
		"Run":                  "'run':*A",
		"run bo":               "'run':*A & 'bo':*A",
		"it's a 'b' | c & !d:": "'it':*A & 's':*A & 'a':*A & 'b':*A & 'c':*A & 'd':*A",
		"Crème-brûlée":         "'crème':*A & 'brûlée':*A",
	} {
		if got := PrefixQuery(typed); got != want {
			t.Errorf("PrefixQuery(%q) = %q, want %q", typed, got, want)
		}
	}
	if got := strings.Count(PrefixQuery(strings.Repeat("w ", 40)), "&"); got != maxQuickWords-1 {
		t.Errorf("%d words kept", got+1)
	}
}

func TestParseQueryCorrectsUnlessAskedForTheWordsAsTyped(t *testing.T) {
	for raw, want := range map[string]bool{"": false, "false": false, "true": true} {
		v := url.Values{"q": {"kubernets"}}
		if raw != "" {
			v.Set("asTyped", raw)
		}
		q, err := ParseQuery(v)
		if err != nil || q.AsTyped != want {
			t.Errorf("asTyped=%q reads as %v (%v), want %v", raw, q.AsTyped, err, want)
		}
	}
}

// The quick search's corrections are looked up for the very words its prefix
// query asks for, so the two must split and lowercase alike.
func TestPrefixWordsAreThoseOfThePrefixQuery(t *testing.T) {
	for typed, want := range map[string][]string{
		"":                {},
		"Kuberntes ru":    {"kuberntes", "ru"},
		"Konfiguraton!?":  {"konfiguraton"},
		"it's 'b' | c":    {"it", "s", "b", "c"},
		"Crème-BRÛLÉE x2": {"crème", "brûlée", "x2"},
	} {
		got := prefixWords(typed)
		if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
			t.Errorf("prefixWords(%q) = %q, want %q", typed, got, want)
		}
		terms := make([]string, len(got))
		for i, w := range got {
			terms[i] = "'" + w + "':*A"
		}
		if q := PrefixQuery(typed); q != strings.Join(terms, " & ") {
			t.Errorf("PrefixQuery(%q) = %q asks for other words than %q", typed, q, got)
		}
	}
	if got := len(prefixWords(strings.Repeat("w ", 40))); got != maxQuickWords {
		t.Errorf("%d words kept", got)
	}
}

func TestCorrectionsAreNoLooserThanTrigramsFindThem(t *testing.T) {
	// pg_trgm's % finds candidates at its similarity_threshold, 0.3, and the
	// vocabulary is only searched through it.
	if SimilarEnough < 0.3 || SimilarEnough >= 1 {
		t.Fatalf("SimilarEnough is %v", SimilarEnough)
	}
}
