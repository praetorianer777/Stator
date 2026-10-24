package oauth

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// The narrower token for one run (#499), adapted from Armature's narrow in
// backend/internal/oauth/token.go with the checks Exciter's #235 added to it.

const (
	// ParamExpiresIn asks the narrower token to end sooner than its subject, in seconds.
	ParamExpiresIn = "stator_expires_in"
	// ParamSpaces limits the narrower token to some of the grant's spaces, by key, space-separated.
	ParamSpaces = "stator_spaces"
	// MaxExchangedTTL is the longest a narrower token is asked to last, a grant's access token's hour.
	MaxExchangedTTL = AccessTokenTTL
)

// errSubject refuses a subject that is not a live access token of the app's own grant.
func errSubject() *Error {
	return refuse(http.StatusBadRequest, "invalid_grant", "The subject_token is not a live access token of this app's own grant. "+
		"Refresh the grant's token and exchange the new one.")
}

// subject is the grant an exchange narrows, as its access token found it.
type subject struct {
	grant, user uuid.UUID
	// held is the grant's api_token scopes: read, or none for one that writes.
	held       []string
	spacesOnly bool
	spaces     []uuid.UUID
	remaining  time.Duration
	member     bool
}

// narrowScope reads the scope an exchange asks for against the grant's: none
// keeps it, and nothing widens it. Both are as an api_token row holds them.
func narrowScope(raw string, held []string) ([]string, bool) {
	allowed := grantScopes(held)
	scopes := splitScope(raw)
	if len(scopes) == 0 {
		return tokenScopes(allowed), true
	}
	if !slices.Contains(scopes, ScopeRead) || slices.ContainsFunc(scopes, func(s string) bool { return !slices.Contains(allowed, s) }) {
		return nil, false
	}
	return tokenScopes(scopes), true
}

// narrowLifetime is what is asked, from a second to MaxExchangedTTL, never
// longer than its subject has left, which is also what none asked gets.
func narrowLifetime(raw string, remaining time.Duration) (time.Duration, bool) {
	if raw == "" {
		return min(remaining, MaxExchangedTTL), true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || time.Duration(n)*time.Second > MaxExchangedTTL {
		return 0, false
	}
	return min(time.Duration(n)*time.Second, remaining), true
}

// spaceKeys reads the spaces asked for as a token's are named: upper case,
// each once, at most auth.MaxTokenSpaces.
func spaceKeys(raw string) ([]string, bool) {
	keys := strings.Fields(strings.ToUpper(raw))
	keys = slices.Compact(slices.Sorted(slices.Values(keys)))
	return keys, len(keys) <= auth.MaxTokenSpaces
}

// narrow trades an access token of the app's own grant for a narrower, shorter
// one, with no refresh token; the grant is untouched.
func (s *Service) narrow(ctx context.Context, client *clientRow, form url.Values) (*TokenResponse, error) {
	keys, ok := spaceKeys(form.Get(ParamSpaces))
	if !ok {
		return nil, refuse(http.StatusBadRequest, "invalid_scope", fmt.Sprintf("Name at most %d spaces in %s.", auth.MaxTokenSpaces, ParamSpaces))
	}
	subjectToken := form.Get("subject_token")
	sub, err := s.subjectOf(ctx, client.ID, subjectToken)
	if err != nil {
		return nil, err
	}
	if !sub.member {
		return nil, ended()
	}
	scopes, ok := narrowScope(form.Get("scope"), sub.held)
	if !ok {
		return nil, refuse(http.StatusBadRequest, "invalid_scope",
			"An exchange may only narrow the grant's scope: ask for read, and at most "+joinScope(grantScopes(sub.held))+".")
	}
	ttl, ok := narrowLifetime(form.Get(ParamExpiresIn), sub.remaining)
	if !ok {
		return nil, refuse(http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("%s is a whole number of seconds from 1 to %d.", ParamExpiresIn, int(MaxExchangedTTL/time.Second)))
	}
	spaces, err := s.reachable(ctx, sub, keys)
	if err != nil {
		return nil, err
	}
	token, digest, err := auth.GeneratePrefixed(auth.AccessTokenPrefix)
	if err != nil {
		return nil, err
	}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// The subject is held while the narrower token is made, so revoking
		// or refreshing it at once cannot be outrun; other exchanges share it.
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM api_token
			WHERE id = $1 AND token_hash = $2 AND client_id = $3 AND access_expires_at > now() AND expires_at > now()
			FOR SHARE`, sub.grant, auth.HashToken(subjectToken), client.ID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return errSubject()
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO oauth_exchanged_token (token_hash, grant_id, scopes, spaces, expires_at)
			SELECT $1, t.id, $3, $4, LEAST(now() + make_interval(secs => $5), t.access_expires_at)
			FROM api_token t WHERE t.id = $2`, digest, id, scopes, spaces, ttl.Seconds())
		return err
	})
	if err != nil {
		return nil, err
	}
	return &TokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: int(ttl / time.Second),
		Scope: joinScope(grantScopes(scopes)), IssuedTokenType: TokenTypeAccess}, nil
}

// subjectOf reads the live grant whose access token an exchange names; a
// token exchanged before is no grant's, so it is refused as any other.
func (s *Service) subjectOf(ctx context.Context, clientID uuid.UUID, token string) (subject, error) {
	if !auth.IsAccessToken(token) || auth.ParseAPIToken(token) != nil {
		return subject{}, errSubject()
	}
	var (
		sub  subject
		left float64
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT t.id, t.user_id, t.scopes, t.spaces_only,
			       ARRAY(SELECT ts.space_id FROM api_token_space ts WHERE ts.token_id = t.id ORDER BY ts.space_id),
			       EXTRACT(EPOCH FROM t.access_expires_at - now())::float8,
			       EXISTS (SELECT 1 FROM member m JOIN app_user u ON u.id = m.user_id
			               WHERE m.user_id = t.user_id AND u.is_active AND m.role <> 'guest')
			FROM api_token t
			WHERE t.token_hash = $1 AND t.client_id = $2 AND t.access_expires_at > now() AND t.expires_at > now()`,
			auth.HashToken(token), clientID).Scan(&sub.grant, &sub.user, &sub.held, &sub.spacesOnly, &sub.spaces, &left, &sub.member)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return subject{}, errSubject()
	}
	if err != nil {
		return subject{}, err
	}
	// Whole seconds, as expires_in counts them, so the answer never promises more than the subject has.
	sub.remaining = time.Duration(math.Floor(left)) * time.Second
	if sub.remaining < time.Second {
		return subject{}, errSubject()
	}
	return sub, nil
}

// reachable resolves the keys as the person within the grant's spaces, so a
// key they cannot see and one the grant does not reach refuse alike.
func (s *Service) reachable(ctx context.Context, sub subject, keys []string) ([]uuid.UUID, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	as := db.WithUser(ctx, sub.user)
	if sub.spacesOnly {
		as = db.WithUserInSpaces(ctx, sub.user, sub.spaces)
	}
	var ids []uuid.UUID
	err := s.db.ReadPrimary(as, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT ARRAY(SELECT id FROM space WHERE key = ANY ($1) ORDER BY id)`, keys).Scan(&ids)
	})
	if err != nil {
		return nil, err
	}
	if len(ids) != len(keys) {
		return nil, refuse(http.StatusBadRequest, "invalid_scope", "Name in "+ParamSpaces+" only spaces the grant reaches and its person may see.")
	}
	return ids, nil
}
