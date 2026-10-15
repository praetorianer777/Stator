package armature

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// everything is what the Armature of api/armature/SOURCE reports.
var everything = []string{"calendar@1", "issue-embed@1", "plan@1", "remote-link-relations@1", "remote-links@1", "reports@1", "theme-export@1", "webhooks@1"}

func metaServer(t *testing.T, calls *atomic.Int32, answer func(w http.ResponseWriter)) string {
	t.Helper()
	return armature(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		if calls != nil {
			calls.Add(1)
		}
		answer(w)
	}).URL
}

func TestMetaReadsWhatArmatureOffers(t *testing.T) {
	base := metaServer(t, nil, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"product":"armature","build":{"version":"1.3.0"},"apiVersion":"1","capabilities":["reports@1","issue-embed@1"]}`))
	})
	meta, err := loopback().Meta(context.Background(), base+"/")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Product != "armature" || meta.Build.Version != "1.3.0" || !slices.Equal(meta.Capabilities, []string{"reports@1", "issue-embed@1"}) {
		t.Errorf("meta = %+v", meta)
	}
}

// An Armature from before /meta answers 404 there, which is no failure: it
// offers nothing Stator can rely on.
func TestAnArmatureWithoutMetaOffersNothing(t *testing.T) {
	base := metaServer(t, nil, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"Not found."}}`))
	})
	_, err := loopback().Meta(context.Background(), base)
	if !errors.Is(err, ErrNoMeta) {
		t.Fatalf("err = %v, want ErrNoMeta", err)
	}
	p := partnerOf(nil, err)
	if p.Status != StatusOK || p.Meta || len(p.Capabilities) != 0 || len(p.Missing) != len(Features) {
		t.Errorf("an Armature without /meta is %+v", p)
	}
	for _, f := range Features {
		if p.Offers(f) {
			t.Errorf("an Armature without /meta offers %s", f)
		}
	}
}

func TestAMissingCapabilityTurnsOffTheFeaturesThatNeedIt(t *testing.T) {
	var meta Meta
	meta.Build.Version = "1.2.0"
	meta.Capabilities = slices.DeleteFunc(slices.Clone(everything), func(c string) bool { return c == CapabilityReports || c == CapabilityIssueEmbed })
	p := partnerOf(&meta, nil)
	want := []MissingFeature{
		{Feature: FeatureIssues, Capability: CapabilityIssueEmbed},
		{Feature: FeatureCreate, Capability: CapabilityIssueEmbed},
		{Feature: FeatureCharts, Capability: CapabilityReports},
	}
	if !slices.Equal(p.Missing, want) {
		t.Errorf("missing = %v, want %v", p.Missing, want)
	}
	if !p.Meta || p.Version == nil || *p.Version != "1.2.0" || !p.Offers(FeatureRoadmap) || p.Offers(FeatureCharts) {
		t.Errorf("partner = %+v", p)
	}

	meta.Capabilities = everything
	if p := partnerOf(&meta, nil); len(p.Missing) != 0 {
		t.Errorf("the pinned Armature lacks %v", p.Missing)
	}
}

// Not knowing is not the same as knowing it is missing: the read itself says
// Armature did not answer.
func TestAnArmatureThatDidNotAnswerIsNotTakenForAnOldOne(t *testing.T) {
	p := partnerOf(nil, ErrUnreachable)
	if p.Status != StatusUnreachable || len(p.Missing) != 0 || !p.Offers(FeatureCharts) {
		t.Errorf("an unreachable Armature is %+v", p)
	}
}

func TestWhatArmatureOffersIsAskedOnceInAWhile(t *testing.T) {
	var calls atomic.Int32
	var reports atomic.Bool
	reports.Store(true)
	base := metaServer(t, &calls, func(w http.ResponseWriter) {
		if reports.Load() {
			_, _ = w.Write([]byte(`{"product":"armature","build":{"version":"1"},"apiVersion":"1","capabilities":["reports@1"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"product":"armature","build":{"version":"1"},"apiVersion":"1","capabilities":[]}`))
	})
	svc := NewService(nil, nil, loopback(), nil, Options{Log: slog.New(slog.DiscardHandler)})
	now := time.Now()
	svc.partners.now = func() time.Time { return now }
	ctx := context.Background()

	if p := svc.Partner(ctx, base, false); !p.Offers(FeatureCharts) {
		t.Fatalf("charts are off: %+v", p)
	}
	reports.Store(false)
	if p := svc.Partner(ctx, base, false); !p.Offers(FeatureCharts) || calls.Load() != 1 {
		t.Errorf("a second read asked Armature again: %d calls", calls.Load())
	}
	if p := svc.Partner(ctx, base, true); p.Offers(FeatureCharts) || calls.Load() != 2 {
		t.Errorf("a fresh read did not ask: %d calls, %+v", calls.Load(), p)
	}
	reports.Store(true)
	now = now.Add(CapabilitiesTTL)
	if p := svc.Partner(ctx, base, false); !p.Offers(FeatureCharts) || calls.Load() != 3 {
		t.Errorf("an answer past CapabilitiesTTL was kept: %d calls", calls.Load())
	}
}

func TestAnArmatureThatDidNotAnswerIsAskedAgainSooner(t *testing.T) {
	c := &partners{entries: map[string]partnerEntry{}, now: time.Now}
	now := time.Now()
	c.now = func() time.Time { return now }
	c.put("a", Partner{Status: StatusUnreachable})
	c.put("b", Partner{Status: StatusOK})
	now = now.Add(CapabilitiesRetry)
	if _, ok := c.get("a"); ok {
		t.Error("an unreachable Armature is not asked again after CapabilitiesRetry")
	}
	if _, ok := c.get("b"); !ok {
		t.Error("an answer was dropped before CapabilitiesTTL")
	}
}
