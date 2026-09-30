package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A person hears once per event, as the first kind of the table that applies
// to them, and the one who acted never hears at all.
func TestPickTellsEachPersonOnceByTheFirstKind(t *testing.T) {
	actor, ann, ben, cat := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	got := Pick(actor, []Tell{
		{UserID: ann, Kind: KindPublished},
		{UserID: actor, Kind: KindMentioned},
		{UserID: ben, Kind: KindCreated},
		{UserID: ann, Kind: KindMentioned},
		{UserID: cat, Kind: KindCommented},
		{UserID: cat, Kind: KindReplied},
		{UserID: ben, Kind: KindCreated},
		{UserID: uuid.Nil, Kind: KindMentioned},
		{UserID: cat, Kind: Kind("unknown")},
	})
	want := []Tell{{UserID: ann, Kind: KindMentioned}, {UserID: ben, Kind: KindCreated}, {UserID: cat, Kind: KindReplied}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tell %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// Hourly bundles go at the next full hour after the oldest row, daily ones
// at the next eight o'clock UTC, and a queue left from before off at once.
func TestDueFollowsTheSchedule(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		digest      Digest
		oldest, now string
		want        bool
	}{
		{DigestOff, "2026-09-30T10:15:00Z", "2026-09-30T10:15:00Z", true},
		{DigestHourly, "2026-09-30T10:15:00Z", "2026-09-30T10:59:59Z", false},
		{DigestHourly, "2026-09-30T10:15:00Z", "2026-09-30T11:00:00Z", true},
		{DigestDaily, "2026-09-30T07:59:00Z", "2026-09-30T07:59:59Z", false},
		{DigestDaily, "2026-09-30T07:59:00Z", "2026-09-30T08:00:00Z", true},
		{DigestDaily, "2026-09-30T08:00:00Z", "2026-09-30T23:00:00Z", false},
		{DigestDaily, "2026-09-30T08:00:00Z", "2026-10-01T08:00:00Z", true},
		{DigestDaily, "2026-09-30T10:00:00+02:00", "2026-10-01T07:00:00Z", false},
	} {
		if got := Due(c.digest, at(c.oldest), at(c.now)); got != c.want {
			t.Errorf("Due(%s, %s, %s) = %v, want %v", c.digest, c.oldest, c.now, got, c.want)
		}
	}
}

// What nobody saved is everything on; a stored map leaves a kind it does not
// name on, so a kind added later reaches everybody.
func TestPreferencesDefaultToOn(t *testing.T) {
	d := DefaultPreferences()
	for _, k := range Kinds {
		if !d.InApp.On(k) || !d.Email.On(k) {
			t.Errorf("%s is off by default", k)
		}
	}
	if d.Digest != DigestOff || !d.AutoWatch {
		t.Errorf("the defaults are %+v", d)
	}
	s, err := switchesFrom([]byte(`{"published": false}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.On(KindPublished) || !s.On(KindMentioned) || !s.On(KindCreated) {
		t.Errorf("a map turning published off reads as %+v", s)
	}
	if back := s.stored(); len(back) != len(Kinds) || back[KindPublished] {
		t.Errorf("the switches store as %v", back)
	}
}

func TestAnUnknownDigestIsRefusedOnItsField(t *testing.T) {
	p := DefaultPreferences()
	p.Digest = "weekly"
	err := p.Validate()
	fe, ok := err.(*FieldError)
	if !ok || fe.Field != "digest" || !strings.HasSuffix(fe.Message, ".") {
		t.Fatalf("weekly is refused with %v", err)
	}
	for _, d := range Digests {
		p.Digest = d
		if err := p.Validate(); err != nil {
			t.Errorf("%s is refused: %v", d, err)
		}
	}
}

// An excerpt is one line of at most MaxExcerptLength characters.
func TestAnExcerptIsOneShortLine(t *testing.T) {
	if got := Excerpt("  Added the\n\nbudget.  "); got != "Added the budget." {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("ä", MaxExcerptLength+50)
	if got := Excerpt(long); len([]rune(got)) != MaxExcerptLength {
		t.Errorf("a long excerpt keeps %d characters", len([]rune(got)))
	}
}

// Mail is the one place the server words a notification, and it links to the
// page and to the preferences.
func TestAMailSaysWhatHappenedAndWhere(t *testing.T) {
	page, thread := uuid.New(), uuid.New()
	m := mailed{kind: KindPublished, actor: "Alice", title: "Runbook", spaceKey: "OPS",
		subject: Subject{PageID: page, Excerpt: "Added the budget."}}.single("bob@example.test", "https://wiki.example")
	if m.Subject != `Alice published a new version of "Runbook"` {
		t.Errorf("the subject is %q", m.Subject)
	}
	for _, want := range []string{"Added the budget.", "https://wiki.example/s/OPS/p/" + page.String(), "https://wiki.example" + PreferencesPath} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("the body lacks %q:\n%s", want, m.Body)
		}
	}
	if got := PageURL("https://wiki.example", "OPS", Subject{PageID: page, ThreadID: &thread}); !strings.HasSuffix(got, "?thread="+thread.String()) {
		t.Errorf("a thread's link is %s", got)
	}
	if got := Sentence(KindCreated, "", "Plans"); got != `Somebody created "Plans"` {
		t.Errorf("a gone actor reads %q", got)
	}
	b := bundle("bob@example.test", "https://wiki.example", []mailed{
		{kind: KindCreated, actor: "Alice", title: "Plans", spaceKey: "OPS", subject: Subject{PageID: page}},
		{kind: KindMentioned, actor: "Carl", title: "Runbook", spaceKey: "OPS", subject: Subject{PageID: page}},
	})
	if b.Subject != "2 updates in Stator" || !strings.Contains(b.Body, `- Carl mentioned you on "Runbook"`) {
		t.Errorf("the bundle is %q:\n%s", b.Subject, b.Body)
	}
}
