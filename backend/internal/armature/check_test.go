package armature

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/netguard"
)

func TestTheGuardSentenceNamesTheValueToSet(t *testing.T) {
	for _, tc := range []struct {
		name     string
		host     string
		resolved []string
		says     []string
		never    []string
	}{
		{
			name: "a public name that resolves to a private address here",
			host: "armature.example.com", resolved: []string{"10.20.30.40"},
			says:  []string{"armature.example.com to network.outboundAllow in the chart values (STATOR_OUTBOUND_ALLOW)", "resolves to a private address"},
			never: []string{"10.20.30.40"},
		},
		{
			name: "a name with a public and a private address",
			host: "armature.example.com", resolved: []string{"203.0.113.7", "fd12::1"},
			says:  []string{"resolves to a private address", "add armature.example.com to network.outboundAllow"},
			never: []string{"203.0.113.7", "fd12::1"},
		},
		{
			name: "a private address typed as such",
			host: "10.11.12.13",
			says: []string{"Stator may not reach 10.11.12.13", "inside the server's own network", "add 10.11.12.13 to network.outboundAllow"},
		},
		{
			name: "a name that resolves to public addresses only, refused after a redirect",
			host: "armature.example.com", resolved: []string{"203.0.113.7"},
			says:  []string{"leads inside the server's own network", "add armature.example.com to network.outboundAllow"},
			never: []string{"resolves to", "203.0.113.7"},
		},
		{
			name: "a name that does not resolve once more",
			host: "armature.internal",
			says: []string{"add armature.internal to network.outboundAllow in the chart values (STATOR_OUTBOUND_ALLOW)"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var addrs []netip.Addr
			for _, a := range tc.resolved {
				addrs = append(addrs, netip.MustParseAddr(a))
			}
			got := BlockedSentence(tc.host, addrs)
			for _, want := range tc.says {
				if !strings.Contains(got, want) {
					t.Errorf("%q does not say %q", got, want)
				}
			}
			for _, leak := range tc.never {
				if strings.Contains(got, leak) {
					t.Errorf("%q tells %q", got, leak)
				}
			}
		})
	}
}

func checker() *Service {
	return NewService(nil, nil, loopback(), nil, Options{Development: true, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func TestACheckReadsTheVersionFromMeta(t *testing.T) {
	base := metaServer(t, nil, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"product":"armature","build":{"version":"1.4.0"},"apiVersion":"1","capabilities":["issue-embed@1"]}`))
	})
	got, err := checker().CheckConnection(context.Background(), ConnectionCheckInput{BaseURL: base + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != base || got.Partner.Version == nil || *got.Partner.Version != "1.4.0" || !got.Partner.Meta {
		t.Errorf("the check is %+v", got)
	}
}

func TestACheckRefusesWhatIsNotArmature(t *testing.T) {
	for name, answer := range map[string]func(w http.ResponseWriter){
		"another product at /meta": func(w http.ResponseWriter) {
			_, _ = w.Write([]byte(`{"product":"exciter","build":{"version":"2.0.0"},"apiVersion":"1","capabilities":[]}`))
		},
		"a page instead of JSON": func(w http.ResponseWriter) { _, _ = w.Write([]byte(`<html></html>`)) },
		"a 403":                  func(w http.ResponseWriter) { w.WriteHeader(http.StatusForbidden) },
	} {
		t.Run(name, func(t *testing.T) {
			base := metaServer(t, nil, answer)
			_, err := checker().CheckConnection(context.Background(), ConnectionCheckInput{BaseURL: base})
			var field *FieldError
			if !errors.As(err, &field) || field.Field != "baseUrl" || !strings.Contains(field.Message, "not Armature") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestACheckOfAGuardedAddressSaysWhatToAllow(t *testing.T) {
	svc := NewService(nil, nil, NewClient(netguard.ParseAllow(""), nil), nil, Options{Development: true, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	_, err := svc.CheckConnection(context.Background(), ConnectionCheckInput{BaseURL: "http://127.0.0.1:9"})
	var field *FieldError
	if !errors.As(err, &field) || !strings.Contains(field.Message, "add 127.0.0.1 to network.outboundAllow in the chart values (STATOR_OUTBOUND_ALLOW)") {
		t.Errorf("err = %v", err)
	}
}
