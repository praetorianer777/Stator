package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// Connecting an app that registered itself to the MCP endpoint (#449),
// adapted from Armature's backend/internal/httpapi/oauth_handlers.go.

const (
	// RegistrationsPerWindow brakes apps registering themselves from one
	// address, each of which an administrator would be shown, as Armature's.
	RegistrationsPerWindow = 10
	// RegistrationWindow is the window that brake counts in.
	RegistrationWindow = 15 * time.Minute
	// consentPath is the web client's consent page, which reads the
	// authorize query whole.
	consentPath = "/oauth/authorize"
	// loginPath is the web client's sign-in, which goes on to next.
	loginPath = "/login"
)

var registrations = newThrottle(RegistrationsPerWindow, RegistrationWindow)

func (s *Server) handleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(oauth.MetadataMaxAge.Seconds())))
	respondJSON(w, r, http.StatusOK, s.OAuth.ResourceMetadata())
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !registrations.gate(w, r, clientIP(r), "Too many apps registered from this address. Wait a few minutes and try again.") {
		return
	}
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	var req oauth.RegistrationRequest
	// RFC 7591 lets an app send metadata Stator does not use, such as a logo;
	// it is read past rather than refused.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		respondOAuthError(w, r, &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_client_metadata", Description: "Send the app's metadata as one JSON object."})
		return
	}
	out, err := s.OAuth.Register(r.Context(), req, clientIP(r))
	if err != nil {
		respondOAuthError(w, r, err)
		return
	}
	respondOAuth(w, http.StatusCreated, out)
}

// handleOAuthAuthorize sends the browser to the consent page, through the
// sign-in when nobody is signed in, the query passed on whole.
func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	base := strings.TrimRight(s.AppBaseURL, "/")
	target := consentPath + "?" + r.URL.RawQuery
	if p := PrincipalFrom(r.Context()); p == nil || p.TokenID != nil {
		target = loginPath + "?next=" + url.QueryEscape(target)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, base+target, http.StatusFound)
}

// consentAnswer is either the page to show or where the browser goes at once.
type consentAnswer struct {
	Consent    *oauth.Consent `json:"consent,omitempty"`
	RedirectTo string         `json:"redirectTo,omitempty"`
}

// consentRequest is the person's decision about the query the page was opened with.
type consentRequest struct {
	Query    string `json:"query"`
	Decision string `json:"decision"`
	// Scope narrows what the app asked for to read; empty keeps it.
	Scope string `json:"scope,omitempty"`
	// Spaces are the keys the grant is limited to; none is every space.
	Spaces []string `json:"spaces,omitempty"`
}

// consentFor reads who is asked and the authorize query, answering the caller
// when the query names no app Stator knows.
func (s *Server) consentFor(w http.ResponseWriter, r *http.Request, query string) (*oauth.Authorization, oauth.Person, bool) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return nil, oauth.Person{}, false
	}
	a, err := s.OAuth.Authorize(r.Context(), query)
	if err != nil {
		respondError(w, r, err)
		return nil, oauth.Person{}, false
	}
	p := PrincipalFrom(r.Context())
	return a, oauth.Person{ID: p.UserID, Name: p.Name, Guest: p.Guest(), Administers: p.CanAdminister()}, true
}

func (s *Server) handleGetOAuthConsent(w http.ResponseWriter, r *http.Request) {
	a, person, ok := s.consentFor(w, r, r.URL.RawQuery)
	if !ok {
		return
	}
	consent, to := s.OAuth.Consent(a, person)
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, r, http.StatusOK, consentAnswer{Consent: consent, RedirectTo: to})
}

func (s *Server) handleDecideOAuthConsent(w http.ResponseWriter, r *http.Request) {
	var req consentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Decision != "allow" && req.Decision != "deny" {
		respondError(w, r, ErrValidation(map[string]string{"decision": "Decide allow or deny."}))
		return
	}
	a, person, ok := s.consentFor(w, r, req.Query)
	if !ok {
		return
	}
	to, lsn, err := s.OAuth.Decide(r.Context(), a, person, oauth.Decision{Allow: req.Decision == "allow", Scope: req.Scope, Spaces: req.Spaces})
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]string{"redirectTo": to})
}
