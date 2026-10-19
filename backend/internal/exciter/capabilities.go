package exciter

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// The capabilities Stator relies on, as Exciter names them at /meta and
// Exciter's #43 agreement lists them.
const (
	CapabilityRequirementEmbed = "requirement-embed@1"
	CapabilityRequirementList  = "requirement-list@1"
	CapabilitySpecifications   = "specifications@1"
	CapabilityWebhooks         = "webhooks@1"
)

// Feature is a part of Stator that needs Exciter to offer a capability.
type Feature string

const (
	// FeatureRequirements are chips and requirement blocks, read by key.
	FeatureRequirements Feature = "requirements"
	// FeatureLists are list blocks, a query in Exciter's query language.
	FeatureLists Feature = "lists"
	// FeatureProjects are the projects a list block's editor offers.
	FeatureProjects Feature = "projects"
	// FeatureWebhooks has Exciter tell Stator when requirements change.
	FeatureWebhooks Feature = "webhooks"
)

// Features lists every Feature, in the order the settings page names them.
var Features = []Feature{FeatureRequirements, FeatureLists, FeatureProjects, FeatureWebhooks}

// FeatureCapabilities is the capability each feature needs Exciter to report.
var FeatureCapabilities = map[Feature]string{
	FeatureRequirements: CapabilityRequirementEmbed,
	FeatureLists:        CapabilityRequirementList,
	FeatureProjects:     CapabilitySpecifications,
	FeatureWebhooks:     CapabilityWebhooks,
}

// MissingFeature is a feature the connected Exciter does not offer what for.
type MissingFeature struct {
	Feature    Feature `json:"feature"`
	Capability string  `json:"capability"`
}

// Partner is what the connected Exciter said of itself at /meta.
type Partner struct {
	// Status is ok when Exciter answered, and unreachable when it did not;
	// then nothing is known to be missing.
	Status Status `json:"status"`
	// Version is the build Exciter named, null when it named none.
	Version *string `json:"version"`
	// Capabilities are the names Exciter reported, sorted.
	Capabilities []string `json:"capabilities"`
	// Missing are the features Stator turns off for this Exciter.
	Missing []MissingFeature `json:"missing"`
}

// Offers says whether Exciter is known to offer what feature needs, by the
// capability's exact name. An Exciter that did not answer is given the
// benefit of the doubt: the read it was asked for says so itself.
func (p *Partner) Offers(feature Feature) bool {
	if p == nil || p.Status != StatusOK {
		return true
	}
	return slices.Contains(p.Capabilities, FeatureCapabilities[feature])
}

// partnerOf judges what an Exciter said at /meta against what Stator needs.
// Exciter has answered /meta since before #43, so one without it offers nothing.
func partnerOf(meta *armature.Meta, err error) Partner {
	p := Partner{Status: StatusOK, Capabilities: []string{}, Missing: []MissingFeature{}}
	switch {
	case errors.Is(err, armature.ErrNoMeta):
	case err != nil:
		p.Status = armature.StatusOf(err)
		return p
	default:
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

// partners keeps each Exciter's answer at /meta in the process.
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
	ttl := armature.CapabilitiesTTL
	if e.partner.Status != StatusOK {
		ttl = armature.CapabilitiesRetry
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

// Partner is what the Exciter at base offers, from the process's copy unless
// fresh or older than armature.CapabilitiesTTL.
func (s *Service) Partner(ctx context.Context, base string, fresh bool) Partner {
	if !fresh {
		if p, ok := s.partners.get(base); ok {
			return p
		}
	}
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	meta, err := s.client.Meta(ctx, base)
	if err != nil && !errors.Is(err, armature.ErrNoMeta) {
		s.opts.Log.Info("Exciter did not say what it offers", "base_url", base, "error", err)
	}
	p := partnerOf(meta, err)
	s.partners.put(base, p)
	return p
}

// ViewerFor is Viewer for a read that needs feature: outdated when the
// connected Exciter does not offer it, so Exciter is not asked in vain.
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
