package armature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestServerMetadataIsHeldToTheConnectedIssuer(t *testing.T) {
	var issuer, methods string
	srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ServerMetadataPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"issuer":"` + issuer + `","authorization_endpoint":"` + issuer + `/oauth/authorize","token_endpoint":"` + issuer +
			`/api/v1/oauth/token","code_challenge_methods_supported":[` + methods + `],"authorization_response_iss_parameter_supported":true}`))
	})
	issuer, methods = srv.URL, `"S256"`
	meta, err := loopback().ServerMetadata(context.Background(), srv.URL+"/")
	if err != nil || meta.TokenEndpoint != srv.URL+"/api/v1/oauth/token" || !meta.ISSParameter {
		t.Fatalf("the metadata is %+v, %v", meta, err)
	}
	for _, tc := range []struct{ name, issuer, methods string }{
		{"another issuer", "https://elsewhere.example", `"S256"`},
		{"no S256", srv.URL, `"plain"`},
	} {
		issuer, methods = tc.issuer, tc.methods
		if _, err := loopback().ServerMetadata(context.Background(), srv.URL); !errors.Is(err, ErrUnreachable) {
			t.Errorf("%s: %v, want it refused", tc.name, err)
		}
	}
}

func TestTheTokenEndpointIsAskedAsOAuthSays(t *testing.T) {
	srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, basic := r.BasicAuth()
		switch {
		case r.Header.Get("Content-Type") != "application/x-www-form-urlencoded":
			w.WriteHeader(http.StatusUnsupportedMediaType)
		case !basic && r.PostForm.Get("client_id") == "public-app" && r.PostForm.Get("grant_type") == "refresh_token":
			_, _ = w.Write([]byte(`{"access_token":"armature_oat_a","token_type":"Bearer","expires_in":3600,"refresh_token":"armature_ort_b","scope":"read"}`))
		// RFC 6749 section 2.3.1: both halves form-encoded before Basic.
		case basic && id == "app+1" && secret == "armature_ocs_s%2Fx" && r.PostForm.Get("code_verifier") == "v":
			_, _ = w.Write([]byte(`{"access_token":"armature_oat_c","token_type":"Bearer","expires_in":3600,"refresh_token":"armature_ort_d","scope":"read issue.write"}`))
		case basic:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"This connection has ended. Connect Armature again."}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Unknown app."}`))
		}
	})
	client := loopback()
	set, err := client.Token(context.Background(), srv.URL, AppCredentials{ClientID: "app 1", ClientSecret: "armature_ocs_s/x"},
		url.Values{"grant_type": {"authorization_code"}, "code_verifier": {"v"}})
	if err != nil || set.AccessToken != "armature_oat_c" || set.Scope != "read issue.write" {
		t.Fatalf("a redemption answered %+v, %v", set, err)
	}
	if set, err := client.Token(context.Background(), srv.URL, AppCredentials{ClientID: "public-app"}, url.Values{"grant_type": {"refresh_token"}}); err != nil || set.RefreshToken != "armature_ort_b" {
		t.Errorf("a public app's refresh answered %+v, %v", set, err)
	}
	_, err = client.Token(context.Background(), srv.URL, AppCredentials{ClientID: "app 1", ClientSecret: "armature_ocs_s/x"}, url.Values{"grant_type": {"refresh_token"}})
	var refused *OAuthError
	if !errors.As(err, &refused) || refused.Code != oauthInvalidGrant || !strings.Contains(refused.Description, "Connect Armature again") {
		t.Errorf("an ended grant is %v, want invalid_grant with Armature's sentence", err)
	}
	if _, err := client.Token(context.Background(), srv.URL, AppCredentials{ClientID: "other"}, url.Values{}); !isOAuth(err, oauthInvalidClient) || errors.Is(err, ErrRejected) {
		t.Errorf("an unknown app is %v, want invalid_client and never a rejected token", err)
	}
}

func TestACallAnswered401IsRenewedOnce(t *testing.T) {
	srv := armature(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer armature_oat_fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"projects":[]}`))
	})
	var renewals atomic.Int32
	renew := func(_ context.Context, rejected string) (string, error) {
		renewals.Add(1)
		if rejected != "armature_oat_old" && rejected != "armature_oat_fresh" {
			t.Errorf("renewed %q", rejected)
		}
		return "armature_oat_fresh", nil
	}
	caller := loopback().Renewing(srv.URL, "armature_oat_old", renew)
	var out map[string]any
	if err := caller.Get(context.Background(), "/projects", nil, &out); err != nil {
		t.Fatalf("a renewed call failed: %v", err)
	}
	if err := caller.Get(context.Background(), "/projects", nil, &out); err != nil || renewals.Load() != 1 {
		t.Errorf("the next call renewed again (%d renewals, %v); the fresh token is kept", renewals.Load(), err)
	}

	ended := loopback().Renewing(srv.URL, "armature_oat_old", func(context.Context, string) (string, error) { return "", ErrRejected })
	if err := ended.Get(context.Background(), "/projects", nil, &out); !errors.Is(err, ErrRejected) {
		t.Errorf("an ended grant answers %v, want ErrRejected", err)
	}
	away := loopback().Renewing(srv.URL, "armature_oat_old", func(context.Context, string) (string, error) { return "", ErrUnreachable })
	if err := away.Get(context.Background(), "/projects", nil, &out); !errors.Is(err, ErrUnreachable) {
		t.Errorf("a renewal Armature did not answer is %v, want unreachable rather than rejected", err)
	}
	if err := loopback().As(srv.URL, "armature_pat_x").Get(context.Background(), "/projects", nil, &out); !errors.Is(err, ErrRejected) {
		t.Errorf("a personal token is not renewed: %v", err)
	}
}

func TestOnlyAReportedCapabilityOffersTheApp(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    *Partner
		want bool
	}{
		{"listed", &Partner{Status: StatusOK, Capabilities: []string{CapabilityPartnerTokens}}, true},
		{"not listed", &Partner{Status: StatusOK, Capabilities: []string{CapabilityIssueEmbed}}, false},
		{"not answering", &Partner{Status: StatusUnreachable}, false},
		{"unknown", nil, false},
	} {
		if got := tc.p.OffersApp(); got != tc.want {
			t.Errorf("%s: OffersApp = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStatesAreLongAndDigestsFixed(t *testing.T) {
	a, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := randomState()
	if a == b || len(a)*6 < 128 {
		t.Errorf("states %q and %q are not 128 bits of their own", a, b)
	}
	if string(digest(a)) != string(digest(a)) || string(digest(a)) == string(digest(b)) {
		t.Error("a digest is not the state's alone")
	}
	site, user := uuid.New(), uuid.New()
	if string(grantContext(site, user, "access")) == string(grantContext(site, user, "refresh")) {
		t.Error("an access token opens as a refresh token")
	}
}
