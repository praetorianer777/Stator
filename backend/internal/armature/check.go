package armature

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/praetorianer777/stator/backend/internal/netguard"
)

// MetaProduct is how an Armature names itself at /meta.
const MetaProduct = "armature"

// OutboundAllowValue and OutboundAllowEnv are where an operator lets a host
// through the outbound guard: the chart value, and the setting it becomes.
const (
	OutboundAllowValue = "network.outboundAllow"
	OutboundAllowEnv   = "STATOR_OUTBOUND_ALLOW"
)

// ConnectionCheckInput is an address the administrator is about to save.
type ConnectionCheckInput struct {
	BaseURL string `json:"baseUrl"`
}

// ConnectionCheck is what answers at an address, before it is saved.
type ConnectionCheck struct {
	// BaseURL is the address as it would be stored.
	BaseURL string `json:"baseUrl"`
	// Partner is what the Armature there said of itself at /meta.
	Partner Partner `json:"partner"`
}

// CheckConnection says whether an Armature answers at the address, and
// what it offers, without storing anything; a refusal is a FieldError on
// baseUrl that names what to do.
func (s *Service) CheckConnection(ctx context.Context, in ConnectionCheckInput) (*ConnectionCheck, error) {
	base, err := s.normalizeBaseURL(in.BaseURL)
	if err != nil {
		return nil, err
	}
	partner, err := s.probe(ctx, base)
	if err != nil {
		return nil, err
	}
	return &ConnectionCheck{BaseURL: base, Partner: partner}, nil
}

// probe asks base what it is: /meta, or for an Armature older than /meta
// its OpenAPI document. What it offers is kept like Partner keeps it.
func (s *Service) probe(ctx context.Context, base string) (Partner, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	meta, err := s.client.Meta(ctx, base)
	var refused *RefusedError
	switch {
	case err == nil && !strings.EqualFold(meta.Product, MetaProduct):
		err = ErrNotArmature
	case errors.Is(err, ErrNoMeta):
		if described := s.client.Describe(ctx, base); described != nil {
			err = described
		}
	case errors.As(err, &refused), errors.Is(err, ErrRejected), errors.Is(err, ErrUnreadable):
		err = ErrNotArmature
	}
	if err != nil && !errors.Is(err, ErrNoMeta) {
		return Partner{}, s.addressError(ctx, base, err)
	}
	p := partnerOf(meta, err)
	s.partners.put(base, p)
	return p, nil
}

// addressError is the sentence for an address Armature could not be asked at.
func (s *Service) addressError(ctx context.Context, base string, err error) error {
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		s.opts.Log.Info("the SSRF guard refused an Armature address", "base_url", base, "error", err)
		host := HostOf(base)
		return &FieldError{Field: "baseUrl", Message: BlockedSentence(host, Resolve(ctx, host))}
	case errors.Is(err, ErrNotArmature):
		return &FieldError{Field: "baseUrl", Message: "Something answers at this address, but it is not Armature. Check the address, and leave out any path such as /api/v1."}
	default:
		s.opts.Log.Info("Armature did not answer at an address being connected", "base_url", base, "error", err)
		return &FieldError{Field: "baseUrl", Message: "Armature did not answer at this address. Check the address, and that Armature is running, then try again."}
	}
}

// HostOf is the lower case host name of an origin, or the origin itself when
// it does not parse.
func HostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	return strings.ToLower(u.Hostname())
}

// Resolve is what host itself resolves to here, and nothing a redirect led
// to, so the sentence tells no more of the network than the name typed.
func Resolve(ctx context.Context, host string) []netip.Addr {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil
	}
	return found
}

// BlockedSentence says why the outbound guard refused host and the exact
// value that lets it through; resolved is what host itself resolves to.
func BlockedSentence(host string, resolved []netip.Addr) string {
	return BlockedSentenceFor("Armature", host, resolved)
}

// BlockedSentenceFor is BlockedSentence for the partner named product, so
// every partner Stator connects is refused in the same words.
func BlockedSentenceFor(product, host string, resolved []netip.Addr) string {
	allow := "Use " + product + "'s public address, or add " + host + " to " + OutboundAllowValue +
		" in the chart values (" + OutboundAllowEnv + "), or ask the operator to."
	if _, err := netip.ParseAddr(host); err == nil {
		return "Stator may not reach " + host + ", because it is an address inside the server's own network. " + allow
	}
	for _, addr := range resolved {
		if netguard.Reserved(addr) {
			return "Stator may not reach " + host + ", because on this server the name resolves to a private address, inside the server's own network, " +
				"which the guard refuses even for a name that is public elsewhere. " + allow
		}
	}
	return "Stator may not reach this address, because it leads inside the server's own network. " + allow
}
