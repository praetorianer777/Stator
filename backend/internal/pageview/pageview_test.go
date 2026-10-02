package pageview

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/config"
)

func TestTheRetentionTheConfigurationNamesIsThisPackages(t *testing.T) {
	if config.DefaultRetainPageViews != DefaultRetention || config.MinRetainPageViews != MinRetention {
		t.Errorf("config keeps %s at least %s, page views %s at least %s",
			config.DefaultRetainPageViews, config.MinRetainPageViews, DefaultRetention, MinRetention)
	}
	if MinRetention < RecentDays*24*time.Hour {
		t.Errorf("views are kept %s, shorter than the recent period of %d days", MinRetention, RecentDays)
	}
}

func TestTheCutoffIsTheFirstDayTheWindowStillHolds(t *testing.T) {
	berlin := time.FixedZone("CEST", 2*60*60)
	for _, tt := range []struct {
		now  time.Time
		keep time.Duration
		want time.Time
	}{
		{time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC), MinRetention, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), MinRetention, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 10, 2, 23, 59, 0, 0, time.UTC), DefaultRetention, time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)},
		// Half past one in Berlin is still the first of October in UTC.
		{time.Date(2026, 10, 2, 1, 30, 0, 0, berlin), MinRetention, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if got := Cutoff(tt.now, tt.keep); !got.Equal(tt.want) {
			t.Errorf("Cutoff(%s, %s) = %s, want %s", tt.now, tt.keep, got, tt.want)
		}
	}
}

func TestTheCountsReadAsTheirFieldsSay(t *testing.T) {
	raw, err := json.Marshal(ViewCounts{Views: 7, Readers: 3, RecentViews: 2, RecentReaders: 1, Days: RecentDays, CanListReaders: true})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"views":7,"readers":3,"recentViews":2,"recentReaders":1,"days":30,"canListReaders":true}`
	if string(raw) != want {
		t.Errorf("the counts read %s, want %s", raw, want)
	}
	raw, err = json.Marshal(Readers{Readers: []Reader{}})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"readers":[],"unnamed":0,"retentionDays":0,"next":null}` {
		t.Errorf("an empty window reads %s", raw)
	}
}
