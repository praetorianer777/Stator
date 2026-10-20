package oauth

import (
	"encoding/base64"
	"errors"
	"net/url"
	"slices"
	"testing"
)

func TestAnAppSaysWhoItIsOneWay(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape("0199-app")+":"+url.QueryEscape("stator_ocs_a+b")))
	cred, err := ReadCredentials(basic, url.Values{})
	if err != nil || cred.ID != "0199-app" || cred.Secret != "stator_ocs_a+b" || !cred.Basic {
		t.Fatalf("Basic reads as %+v, %v", cred, err)
	}
	if cred, err := ReadCredentials("", url.Values{"client_id": {"0199-app"}, "client_secret": {"s"}}); err != nil || cred.Basic || cred.Secret != "s" {
		t.Errorf("the form reads as %+v, %v", cred, err)
	}
	for why, c := range map[string]struct {
		header string
		form   url.Values
		code   string
	}{
		"both ways":        {basic, url.Values{"client_secret": {"s"}}, "invalid_request"},
		"no secret":        {"", url.Values{"client_id": {"0199-app"}}, "invalid_client"},
		"unreadable Basic": {"Basic !!!", url.Values{}, "invalid_client"},
	} {
		_, err := ReadCredentials(c.header, c.form)
		var refused *Error
		if !errors.As(err, &refused) || refused.Code != c.code {
			t.Errorf("%s answered %v, want %s", why, err, c.code)
		}
	}
}

func TestAScopeAskedIsHeldToWhatTheAppMayHave(t *testing.T) {
	both := []string{ScopeRead, ScopeWrite}
	if got, err := asked("", both); err != nil || !slices.Equal(got, both) {
		t.Errorf("no scope asked gives %v, %v", got, err)
	}
	if got, err := asked("read read", both); err != nil || !slices.Equal(got, []string{ScopeRead}) {
		t.Errorf("read asked twice gives %v, %v", got, err)
	}
	for _, refused := range []string{"write", "read admin"} {
		if _, err := asked(refused, both); err == nil {
			t.Errorf("%q was granted", refused)
		}
	}
	if _, err := asked("read write", []string{ScopeRead}); err == nil {
		t.Error("write was granted to an app that may only read")
	}
}

// A grant is an api_token, whose scopes say read for a reading token and
// nothing for one that may do what its person may.
func TestAGrantsScopesAreATokensScopes(t *testing.T) {
	if got := tokenScopes([]string{ScopeRead}); !slices.Equal(got, []string{ScopeRead}) {
		t.Errorf("read is the token scopes %v", got)
	}
	if got := tokenScopes([]string{ScopeRead, ScopeWrite}); len(got) != 0 {
		t.Errorf("read write is the token scopes %v", got)
	}
	if got := grantScopes([]string{}); !slices.Equal(got, []string{ScopeRead, ScopeWrite}) {
		t.Errorf("a full token is the grant scopes %v", got)
	}
}

func TestAnAppsScopesNameRead(t *testing.T) {
	if got, err := normalScopes("scopes", []string{"write", "read", "write"}); err != nil || !slices.Equal(got, []string{ScopeRead, ScopeWrite}) {
		t.Errorf("read and write normalise to %v, %v", got, err)
	}
	for _, refused := range [][]string{{"write"}, {"read", "admin"}, {}} {
		var field *FieldError
		if _, err := normalScopes("scopes", refused); !errors.As(err, &field) || field.Field != "scopes" {
			t.Errorf("%v answered %v", refused, err)
		}
	}
}

func TestTheMetadataNamesEveryAddressUnderTheIssuer(t *testing.T) {
	m := NewService(nil, nil, "https://wiki.example/", nil).Metadata("/api/v1")
	if m.Issuer != "https://wiki.example" || m.TokenEndpoint != "https://wiki.example/api/v1/oauth/token" ||
		m.RevocationEndpoint != "https://wiki.example/api/v1/oauth/revoke" || !slices.Equal(m.GrantTypesSupported, GrantTypes) {
		t.Errorf("the metadata is %+v", m)
	}
}
