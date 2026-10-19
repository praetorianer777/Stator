package armature

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
)

func TestOnlyAnArmatureThatSaysSoIsAskedAtSignIn(t *testing.T) {
	for _, tc := range []struct {
		why  string
		p    *Partner
		want bool
	}{
		{"it lists the capability", &Partner{Status: StatusOK, Capabilities: []string{CapabilityPartnerTokens, CapabilityTokenExchange}}, true},
		{"it lists connected apps alone", &Partner{Status: StatusOK, Capabilities: []string{CapabilityPartnerTokens}}, false},
		{"it did not answer", &Partner{Status: StatusUnreachable, Capabilities: []string{CapabilityTokenExchange}}, false},
		{"nothing is known", nil, false},
	} {
		if got := tc.p.OffersTokenExchange(); got != tc.want {
			t.Errorf("when %s the exchange is offered: %v, want %v", tc.why, got, tc.want)
		}
	}
}

func TestARefusalAtSignInIsSaidInArmaturesWords(t *testing.T) {
	said := "Ada Lovelace has no Armature account yet. Sign in to Armature once, or ask an administrator to let them in."
	if got := signInRefusal(&OAuthError{Status: 400, Code: oauthInvalidGrant, Description: "  " + said + " "}); got != said {
		t.Errorf("Armature's sentence came out as %q", got)
	}
	if got := signInRefusal(&OAuthError{Status: 400, Code: "unauthorized_client"}); !strings.Contains(got, "(unauthorized_client)") || !strings.Contains(got, "Connect Armature below") {
		t.Errorf("a refusal without a sentence reads %q", got)
	}
}

// Off, or without the person or their token, nothing is asked: the service
// has no database here, so reaching it would panic.
func TestNothingIsAskedAtSignInUnlessTurnedOn(t *testing.T) {
	off := NewService(nil, nil, nil, nil, Options{})
	if lsn := off.ConnectAtSignIn(db.WithUser(context.Background(), uuid.New()), "a token"); lsn != 0 {
		t.Errorf("the exchange ran while off and wrote %d", lsn)
	}
	on := NewService(nil, nil, nil, nil, Options{TokenExchange: true})
	if lsn := on.ConnectAtSignIn(context.Background(), "a token"); lsn != 0 {
		t.Errorf("the exchange ran for nobody and wrote %d", lsn)
	}
	if lsn := on.ConnectAtSignIn(db.WithUser(context.Background(), uuid.New()), ""); lsn != 0 {
		t.Errorf("the exchange ran without a token and wrote %d", lsn)
	}
}
