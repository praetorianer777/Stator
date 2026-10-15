package armature

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// MetaPath is where an Armature says which build it is and what it offers,
// to anybody; an Armature from before it answers 404 there.
const MetaPath = "/meta"

// The capabilities Stator relies on, as Armature names them at /meta. The
// names and what each promises are Dynamo's shared list (docs/integration.md).
const (
	CapabilityIssueEmbed  = "issue-embed@1"
	CapabilityRemoteLinks = "remote-links@1"
	CapabilityWebhooks    = "webhooks@1"
	CapabilityThemeExport = "theme-export@1"
	CapabilityReports     = "reports@1"
	CapabilityPlan        = "plan@1"
	CapabilityCalendar    = "calendar@1"
)

const (
	// CapabilitiesTTL is how long an Armature's answer at /meta is trusted.
	// Nothing announces an upgrade, and the settings page asks afresh.
	CapabilitiesTTL = 5 * time.Minute
	// CapabilitiesRetry is how long an Armature that did not answer at /meta
	// is left alone, so every read does not wait for it twice.
	CapabilitiesRetry = 30 * time.Second
)

// Feature is a part of Stator that needs Armature to offer a capability.
type Feature string

const (
	// FeatureIssues are chips, issue blocks, list blocks and the picker.
	FeatureIssues Feature = "issues"
	// FeatureCreate files issues from a selection; Armature names no
	// capability for filing, so it rests on the chips it leaves behind.
	FeatureCreate   Feature = "create"
	FeatureCharts   Feature = "charts"
	FeatureRoadmap  Feature = "roadmap"
	FeatureCalendar Feature = "calendar"
	// FeatureLinks puts a page's remote link on the issues it names.
	FeatureLinks Feature = "links"
	// FeatureTheme follows the theme a person chose in Armature.
	FeatureTheme Feature = "theme"
	// FeatureWebhooks has Armature tell Stator when issues change.
	FeatureWebhooks Feature = "webhooks"
)

// Features lists every Feature, in the order the settings page names them.
var Features = []Feature{FeatureIssues, FeatureCreate, FeatureCharts, FeatureRoadmap, FeatureCalendar, FeatureLinks, FeatureTheme, FeatureWebhooks}

// FeatureCapabilities is the capability each feature needs Armature to report.
var FeatureCapabilities = map[Feature]string{
	FeatureIssues:   CapabilityIssueEmbed,
	FeatureCreate:   CapabilityIssueEmbed,
	FeatureCharts:   CapabilityReports,
	FeatureRoadmap:  CapabilityPlan,
	FeatureCalendar: CapabilityCalendar,
	FeatureLinks:    CapabilityRemoteLinks,
	FeatureTheme:    CapabilityThemeExport,
	FeatureWebhooks: CapabilityWebhooks,
}

// MissingFeature is a feature the connected Armature does not offer what for.
type MissingFeature struct {
	Feature    Feature `json:"feature"`
	Capability string  `json:"capability"`
}

// Partner is what the connected Armature said of itself at /meta.
type Partner struct {
	// Status is ok when Armature answered, with /meta or with a 404 for none,
	// and unreachable when it did not; then nothing is known to be missing.
	Status Status `json:"status"`
	// Meta is false for an Armature too old to answer /meta at all.
	Meta bool `json:"meta"`
	// Version is the build Armature named, null when it named none.
	Version *string `json:"version"`
	// Capabilities are the names Armature reported, sorted.
	Capabilities []string `json:"capabilities"`
	// Missing are the features Stator turns off for this Armature.
	Missing []MissingFeature `json:"missing"`
}

// Offers says whether Armature is known to offer what feature needs. An
// Armature that did not answer is given the benefit of the doubt: the read
// it was asked for says so itself.
func (p *Partner) Offers(feature Feature) bool {
	if p == nil || p.Status != StatusOK {
		return true
	}
	return slices.Contains(p.Capabilities, FeatureCapabilities[feature])
}

// partnerOf judges what an Armature said at /meta against what Stator needs.
func partnerOf(meta *Meta, err error) Partner {
	p := Partner{Status: StatusOK, Capabilities: []string{}, Missing: []MissingFeature{}}
	switch {
	case errors.Is(err, ErrNoMeta):
	case err != nil:
		p.Status = StatusOf(err)
		return p
	default:
		p.Meta = true
		if meta.Build.Version != "" {
			p.Version = &meta.Build.Version
		}
		p.Capabilities = append(p.Capabilities, meta.Capabilities...)
		slices.Sort(p.Capabilities)
	}
	for _, f := range Features {
		if !p.Offers(f) {
			p.Missing = append(p.Missing, MissingFeature{Feature: f, Capability: FeatureCapabilities[f]})
		}
	}
	return p
}

// Meta is an Armature's answer at /meta, the shape Stator and Exciter share.
type Meta struct {
	Product string `json:"product"`
	Build   struct {
		Version string `json:"version"`
	} `json:"build"`
	APIVersion   string   `json:"apiVersion"`
	Capabilities []string `json:"capabilities"`
}

// ErrNoMeta is an Armature that answers, but not at /meta: one older than
// the capabilities, which Stator reads as offering none.
var ErrNoMeta = errors.New("Armature does not answer /meta")

// Meta asks the Armature at baseURL what it offers; /meta needs no token.
func (c *Client) Meta(ctx context.Context, baseURL string) (*Meta, error) {
	var out Meta
	err := c.do(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+APIPath+MetaPath, "", nil, &out)
	var refused *RefusedError
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusMethodNotAllowed) {
		return nil, ErrNoMeta
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// partners keeps each Armature's answer at /meta in the process.
type partners struct {
	mu      sync.Mutex
	entries map[string]partnerEntry
	now     func() time.Time
}

type partnerEntry struct {
	at      time.Time
	partner Partner
}

func (c *partners) get(base string) (Partner, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[base]
	if !ok {
		return Partner{}, false
	}
	ttl := CapabilitiesTTL
	if e.partner.Status != StatusOK {
		ttl = CapabilitiesRetry
	}
	if age := c.now().Sub(e.at); age < 0 || age >= ttl {
		return Partner{}, false
	}
	return e.partner, true
}

func (c *partners) put(base string, p Partner) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[base] = partnerEntry{at: c.now(), partner: p}
}

// Partner is what the Armature at base offers, from the process's copy
// unless fresh or older than CapabilitiesTTL.
func (s *Service) Partner(ctx context.Context, base string, fresh bool) Partner {
	if !fresh {
		if p, ok := s.partners.get(base); ok {
			return p
		}
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	meta, err := s.client.Meta(ctx, base)
	if err != nil && !errors.Is(err, ErrNoMeta) {
		s.opts.Log.Info("Armature did not say what it offers", "base_url", base, "error", err)
	}
	p := partnerOf(meta, err)
	s.partners.put(base, p)
	return p
}

// ViewerFor is Viewer for a read that needs feature: outdated when the
// connected Armature does not offer it, so Armature is not asked in vain.
func (s *Service) ViewerFor(ctx context.Context, feature Feature) (*Viewer, Status, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return v, status, err
	}
	if p := s.Partner(ctx, v.Caller.BaseURL(), false); !p.Offers(feature) {
		return nil, StatusOutdated, nil
	}
	return v, status, nil
}

// OutdatedError is a write that needs a capability the connected Armature
// does not report.
type OutdatedError struct {
	Feature    Feature
	Capability string
}

func (e *OutdatedError) Error() string {
	return "The connected Armature does not offer " + e.Capability + ", which Stator needs for this. Ask an administrator to update Armature to a release that offers it."
}
