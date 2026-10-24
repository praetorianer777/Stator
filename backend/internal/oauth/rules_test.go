package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"
)

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestPKCEHoldsAVerifierToItsChallenge(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	if !VerifyPKCE(challengeOf(verifier), verifier) {
		t.Fatal("the verifier a challenge was made from is refused")
	}
	for why, c := range map[string][2]string{
		"another verifier":    {challengeOf(verifier), strings.Repeat("b", 43)},
		"a short verifier":    {challengeOf("short"), "short"},
		"a plain challenge":   {verifier, verifier},
		"a long verifier":     {challengeOf(strings.Repeat("a", 129)), strings.Repeat("a", 129)},
		"a verifier's spaces": {challengeOf(strings.Repeat("a ", 22)), strings.Repeat("a ", 22)},
	} {
		if VerifyPKCE(c[0], c[1]) {
			t.Errorf("%s passes", why)
		}
	}
}

func TestAReturnAddressIsHTTPSOrLoopback(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://rotor.example/oauth/callback": true,
		"http://127.0.0.1:8123/callback":       true,
		"http://localhost/callback":            true,
		"http://[::1]:9000/cb":                 true,
		"http://rotor.example/oauth/callback":  false,
		"https://rotor.example/cb#fragment":    false,
		"https://user:pass@rotor.example/cb":   false,
		"/oauth/callback":                      false,
		"javascript:alert(1)":                  false,
		"https://":                             false,
	} {
		if got := validRedirect(raw); got != want {
			t.Errorf("%q is valid %v, want %v", raw, got, want)
		}
	}
}

func TestAReturnAddressMatchesWhatWasRegistered(t *testing.T) {
	registered := []string{"https://rotor.example/cb", "http://127.0.0.1:8000/cb"}
	for asked, want := range map[string]bool{
		"https://rotor.example/cb":       true,
		"http://127.0.0.1:51234/cb":      true,
		"https://rotor.example/cb?x=1":   false,
		"https://rotor.example:8443/cb":  false,
		"http://127.0.0.1:51234/other":   false,
		"http://localhost:51234/cb":      false,
		"https://evil.example/cb":        false,
		"http://127.0.0.1:51234/cb#frag": false,
	} {
		if got := RedirectMatches(registered, asked); got != want {
			t.Errorf("%q matches %v, want %v", asked, got, want)
		}
	}
}

func TestARegistrationIsHeldToTheRules(t *testing.T) {
	if got, err := checkGrants(nil); err != nil || !slices.Equal(got, []string{GrantAuthorizationCode, GrantRefreshToken}) {
		t.Errorf("no grants give %v, %v", got, err)
	}
	// Rotor asks for the exchange when the metadata lists it.
	if got, err := checkGrants([]string{GrantAuthorizationCode, GrantRefreshToken, GrantTokenExchange}); err != nil || len(got) != 3 {
		t.Errorf("the exchange beside the code gives %v, %v", got, err)
	}
	for why, grants := range map[string][]string{
		"no code":  {GrantRefreshToken, GrantTokenExchange},
		"password": {GrantAuthorizationCode, "password"},
		"implicit": {"implicit"},
	} {
		var refused *Error
		if _, err := checkGrants(grants); !errors.As(err, &refused) || refused.Code != "invalid_client_metadata" {
			t.Errorf("%s answered %v", why, err)
		}
	}
	if _, err := checkRedirects(nil); err == nil || err.Code != "invalid_redirect_uri" {
		t.Errorf("no return address answered %v", err)
	}
	if _, err := checkRedirects(slices.Repeat([]string{"https://a.example/cb"}, MaxRedirectURIs+1)); err == nil {
		t.Error("too many return addresses passed")
	}
	if got, err := checkRedirects([]string{"https://a.example/cb", "https://a.example/cb"}); err != nil || len(got) != 1 {
		t.Errorf("a repeated address gives %v, %v", got, err)
	}
	if _, err := checkClientURI("ftp://a.example"); err == nil {
		t.Error("an ftp client_uri passed")
	}
}

// The authorization request is held to Armature's rules in Armature's order.
func TestAnAuthorizationsScopeDefaultsToWhatTheAppMayHave(t *testing.T) {
	if got, err := asked("", []string{ScopeRead}); err != nil || !slices.Equal(got, []string{ScopeRead}) {
		t.Errorf("no scope from a reading app gives %v, %v", got, err)
	}
	if _, err := asked("write", Scopes); err == nil {
		t.Error("write without read passed")
	}
}

func TestAnAnswerKeepsTheReturnAddressesQuery(t *testing.T) {
	a := &Authorization{redirectURI: "https://rotor.example/cb?tool=stator", state: "s1"}
	got := a.errorRedirect("https://wiki.example", refuse(400, "access_denied", "No."))
	for _, want := range []string{"tool=stator", "state=s1", "error=access_denied", "iss=https%3A%2F%2Fwiki.example"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %s", got, want)
		}
	}
}

func TestAPersonIsAskedOnlyWhatTheyMayDecide(t *testing.T) {
	s := NewService(nil, nil, "https://wiki.example", nil)
	app := func(status string) *Authorization {
		return &Authorization{client: &clientRow{Name: "Rotor", Status: status, Kind: KindRegistered},
			redirectURI: "https://rotor.example/cb", state: "s", scopes: Scopes}
	}
	member, admin := Person{Name: "Alice"}, Person{Name: "Ada", Administers: true}
	if c, to := s.Consent(app(StatusPending), member); to != "" || c.MayAllow || c.MayCancel || !strings.Contains(c.Refusal, "administrator must allow Rotor") {
		t.Errorf("a member is shown a pending app as %+v, %q", c, to)
	}
	if c, _ := s.Consent(app(StatusPending), admin); !c.MayAllow || !c.AllowsApp {
		t.Errorf("an administrator is shown a pending app as %+v", c)
	}
	if c, _ := s.Consent(app(StatusAllowed), member); !c.MayAllow || c.AllowsApp || c.Refusal != "" {
		t.Errorf("a member is shown an allowed app as %+v", c)
	}
	if c, _ := s.Consent(app(StatusAllowed), Person{Name: "Gus", Guest: true}); c.MayAllow || !c.MayCancel {
		t.Errorf("a guest is shown %+v", c)
	}
	broken := app(StatusAllowed)
	broken.problem = refuse(400, "invalid_request", "Send state.")
	if _, to := s.Consent(broken, member); !strings.HasPrefix(to, "https://rotor.example/cb?") {
		t.Errorf("a broken request to an allowed app goes to %q", to)
	}
	// An app nobody allowed borrows no redirect from Stator.
	broken.client.Status = StatusPending
	if c, to := s.Consent(broken, admin); to != "" || c.MayAllow || c.Refusal != "Send state." {
		t.Errorf("a broken request to a pending app answers %+v, %q", c, to)
	}
}
