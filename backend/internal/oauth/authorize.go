package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// The authorization endpoint and its consent page, adapted from Armature's
// backend/internal/oauth/authorize.go, with spaces to limit a grant to.

// ErrUnknownApp is a client or a return address Stator does not know, which
// is never redirected to.
var ErrUnknownApp = errors.New("the app or its return address is not registered")

// Authorization is an authorize request whose app and return address are
// known. Anything else wrong with it is answered at that address.
type Authorization struct {
	client      *clientRow
	redirectURI string
	state       string
	scopes      []string
	challenge   string
	resource    string
	// problem is answered at the return address instead of a page.
	problem *Error
}

// Person is who is asked to connect an app.
type Person struct {
	ID   uuid.UUID
	Name string
	// Guest is a guest of one space, who has no account to lend an app.
	Guest bool
	// Administers is whether the person may allow an app nobody allowed yet.
	Administers bool
}

// Consent is what the consent page shows.
type Consent struct {
	Client       ConsentClient `json:"client"`
	RedirectHost string        `json:"redirectHost"`
	Person       string        `json:"person"`
	// Scopes are what the app asks for: read, and write when it may change pages.
	Scopes []string `json:"scopes"`
	// Pending is an app no administrator allowed yet; AllowsApp says this
	// person's consent is what allows it.
	Pending   bool `json:"pending"`
	AllowsApp bool `json:"allowsApp"`
	// MayAllow is false when the person may only cancel, or not even that,
	// and Refusal says why.
	MayAllow  bool   `json:"mayAllow"`
	MayCancel bool   `json:"mayCancel"`
	Refusal   string `json:"refusal,omitempty"`
}

// ConsentClient is the app as the consent page names it.
type ConsentClient struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	ClientURI string    `json:"clientUri,omitempty"`
}

// Decision is the person's answer on the consent page.
type Decision struct {
	// Allow is false for cancelling.
	Allow bool
	// Scope narrows what the app asked for, to read; empty keeps it.
	Scope string
	// Spaces are space keys the grant is limited to; none is every space.
	Spaces []string
}

// RefusedError is a decision the person may not make, with the sentence why.
type RefusedError struct{ Sentence string }

func (e *RefusedError) Error() string { return e.Sentence }

// authorizeParams may each be sent once at most.
var authorizeParams = []string{"response_type", "client_id", "state", "scope", "code_challenge", "code_challenge_method", "resource"}

// Authorize reads an authorize query. An unknown app and a return address it
// did not register read the same, and are never redirected to.
func (s *Service) Authorize(ctx context.Context, rawQuery string) (*Authorization, error) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, ErrUnknownApp
	}
	id, err := uuid.Parse(q.Get("client_id"))
	if err != nil {
		return nil, ErrUnknownApp
	}
	var client *clientRow
	err = s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		client, err = s.loadClient(ctx, tx, id)
		return err
	})
	if errors.Is(err, ErrNoSuchClient) {
		return nil, ErrUnknownApp
	}
	if err != nil {
		return nil, err
	}
	a := &Authorization{client: client, redirectURI: q.Get("redirect_uri"), state: q.Get("state"),
		challenge: q.Get("code_challenge"), resource: q.Get("resource")}
	if a.redirectURI == "" && len(client.RedirectURIs) == 1 {
		a.redirectURI = client.RedirectURIs[0]
	}
	if client.Kind != KindRegistered || len(q["redirect_uri"]) > 1 || !RedirectMatches(client.RedirectURIs, a.redirectURI) {
		return nil, ErrUnknownApp
	}
	for _, name := range authorizeParams {
		if len(q[name]) > 1 {
			a.problem = refuse(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Send %s once.", name))
			return a, nil
		}
	}
	a.scopes, err = asked(q.Get("scope"), client.Scopes)
	switch {
	case q.Get("response_type") != ResponseCode:
		a.problem = refuse(http.StatusBadRequest, "unsupported_response_type", "Stator answers response_type=code only.")
	case a.state == "":
		a.problem = refuse(http.StatusBadRequest, "invalid_request", "Send a state of your own, so the answer can be tied to the request.")
	case q.Get("code_challenge_method") != ChallengeS256 || !challengeShape.MatchString(a.challenge):
		a.problem = refuse(http.StatusBadRequest, "invalid_request", "Stator requires PKCE: send a code_challenge with code_challenge_method S256.")
	case err != nil:
		a.problem = refuse(http.StatusBadRequest, "invalid_scope", fmt.Sprintf("Ask for read, and at most %s.", joinScope(client.Scopes)))
	case a.resource != "" && !s.resourceMatches(a.resource):
		a.problem = refuse(http.StatusBadRequest, "invalid_target", "The one resource is Stator's MCP endpoint, "+s.Resource()+".")
	}
	return a, nil
}

// Consent is what the page shows the person, or where the browser goes at
// once: an error answered at the app.
func (s *Service) Consent(a *Authorization, person Person) (*Consent, string) {
	c := &Consent{
		Client:       ConsentClient{ID: a.client.ID, Name: a.client.Name, ClientURI: a.client.ClientURI},
		RedirectHost: redirectHost(a.redirectURI),
		Person:       person.Name,
		Scopes:       a.scopes,
		Pending:      a.client.Status == StatusPending,
	}
	if c.Scopes == nil {
		c.Scopes = []string{}
	}
	name := a.client.Name
	switch {
	case person.Guest:
		c.Refusal, c.MayCancel = "A connected app acts as a member of Stator, and a guest cannot lend it their account. Sign in as a member.", true
	// An app nobody allowed is not sent anybody back, so its address
	// cannot borrow Stator's for a redirect.
	case a.problem != nil && c.Pending:
		c.Refusal = a.problem.Description
	case a.problem != nil:
		return nil, a.errorRedirect(s.issuer, a.problem)
	case c.Pending && !person.Administers:
		c.Refusal = fmt.Sprintf("A Stator administrator must allow %s first. Ask one to connect it once; then you can connect it too.", name)
	default:
		c.MayAllow, c.MayCancel, c.AllowsApp = true, true, c.Pending
	}
	return c, ""
}

// Decide carries out the person's answer and says where the browser goes.
// Allowing a pending app, which only an administrator may, allows it for all.
func (s *Service) Decide(ctx context.Context, a *Authorization, person Person, d Decision) (string, db.LSN, error) {
	c, to := s.Consent(a, person)
	switch {
	case to != "":
		return to, 0, nil
	case !d.Allow && !c.MayCancel, d.Allow && !c.MayAllow:
		return "", 0, &RefusedError{Sentence: c.Refusal}
	case !d.Allow:
		return a.errorRedirect(s.issuer, refuse(http.StatusBadRequest, "access_denied", person.Name+" did not connect "+a.client.Name+".")), 0, nil
	}
	scopes := a.scopes
	if d.Scope != "" {
		narrowed, err := asked(d.Scope, a.scopes)
		if err != nil {
			return "", 0, &FieldError{Field: "scope", Message: fmt.Sprintf("Allow read, or %s, as %s asked.", joinScope(a.scopes), a.client.Name)}
		}
		scopes = narrowed
	}
	spaces, err := s.spacesOf(ctx, d.Spaces)
	if err != nil {
		return "", 0, err
	}
	var lsn db.LSN
	if c.AllowsApp {
		if lsn, err = s.allowApp(ctx, a.client, person); err != nil {
			return "", 0, err
		}
	}
	to, issued, err := s.issueCode(ctx, a, person, scopes, spaces, d.Spaces)
	return to, max(lsn, issued), err
}

// spacesOf reads the spaces a grant is limited to as the person, so a key
// they cannot see is refused rather than widening the grant.
func (s *Service) spacesOf(ctx context.Context, keys []string) ([]uuid.UUID, error) {
	normal := []string{}
	for _, k := range keys {
		if k = strings.ToUpper(strings.TrimSpace(k)); k != "" && !slices.Contains(normal, k) {
			normal = append(normal, k)
		}
	}
	if len(normal) == 0 {
		return nil, nil
	}
	if len(normal) > auth.MaxTokenSpaces {
		return nil, &FieldError{Field: "spaces", Message: fmt.Sprintf("Limit the app to at most %d spaces, or let it reach every space you do.", auth.MaxTokenSpaces)}
	}
	ids := []uuid.UUID{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id FROM space WHERE key = ANY ($1)`, normal)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err == nil && len(ids) != len(normal) {
		err = &FieldError{Field: "spaces", Message: "Choose only spaces you can see; one of them is not among them any more."}
	}
	return ids, err
}

// allowApp is an administrator allowing a pending app, as themselves under
// the policy that lets administrators alone change an app.
func (s *Service) allowApp(ctx context.Context, client *clientRow, person Person) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE oauth_client SET status = 'allowed' WHERE id = $1 AND status = 'pending'`, client.ID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthClientAllowed, TargetType: "oauth_client", TargetID: &client.ID, Actor: person.ID,
			Data: map[string]any{"name": client.Name, "redirectUris": client.RedirectURIs}})
	})
}

// issueCode makes a code for the consent and the address that carries it.
func (s *Service) issueCode(ctx context.Context, a *Authorization, person Person, scopes []string, spaces []uuid.UUID, keys []string) (string, db.LSN, error) {
	code, digest, err := auth.GeneratePrefixed(auth.CodePrefix)
	if err != nil {
		return "", 0, err
	}
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO oauth_code (code_hash, client_id, user_id, scopes, spaces, redirect_uri, challenge, resource, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			digest, a.client.ID, person.ID, scopes, spaces, a.redirectURI, a.challenge, a.resource, s.now().Add(CodeTTL)); err != nil {
			return notAllowed(err, a.client.Name)
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthGrantConsented, TargetType: "oauth_client", TargetID: &a.client.ID, Actor: person.ID,
			Data: map[string]any{"app": a.client.Name, "scopes": scopes, "spaces": keys}})
	})
	if err != nil {
		return "", 0, err
	}
	return a.redirect(url.Values{"code": {code}, "iss": {s.issuer}}), lsn, nil
}

// notAllowed is the database refusing a code for an app nobody allowed.
func notAllowed(err error, name string) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "oauth_client_allowed" {
		return &RefusedError{Sentence: fmt.Sprintf("A Stator administrator must allow %s first. Ask one to connect it once; then you can connect it too.", name)}
	}
	return err
}

// errorRedirect answers a problem at the app's return address (RFC 9207's iss
// included, as on success).
func (a *Authorization) errorRedirect(issuer string, e *Error) string {
	return a.redirect(url.Values{"error": {e.Code}, "error_description": {e.Description}, "iss": {issuer}})
}

// redirect is the return address with the answer and the state added to the
// query it already has.
func (a *Authorization) redirect(add url.Values) string {
	u, err := url.Parse(a.redirectURI)
	if err != nil {
		return a.redirectURI
	}
	q := u.Query()
	for k, v := range add {
		q[k] = v
	}
	if a.state != "" {
		q.Set("state", a.state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
