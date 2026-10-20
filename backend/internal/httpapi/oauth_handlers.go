package httpapi

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// Stator as the authorization server partner tools exchange a person's
// identity provider token at (#461). The token and revocation endpoints and
// the metadata answer OAuth's own shapes; the apps' administration answers
// Stator's envelope.

// errOAuthOff is a build without the service, as the suites' smaller servers are.
var errOAuthOff = &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "Connected apps are not served here."}

// respondOAuth answers an OAuth endpoint, which is never cached.
func respondOAuth(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

// respondOAuthError answers a refusal in OAuth's shape; anything else is a
// fault of Stator's, said as server_error without its details.
func respondOAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var refused *oauth.Error
	if !errors.As(err, &refused) {
		loggerFrom(r.Context()).Error("an OAuth request failed", "error", err)
		refused = &oauth.Error{Status: http.StatusInternalServerError, Code: "server_error", Description: "Stator could not answer. Try again in a moment."}
	}
	if refused.Basic && refused.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="stator"`)
	}
	respondOAuth(w, refused.Status, refused)
}

// oauthForm reads an OAuth endpoint's form, which is all it takes.
func oauthForm(w http.ResponseWriter, r *http.Request) error {
	if kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || kind != "application/x-www-form-urlencoded" {
		return &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_request", Description: "Send the request as application/x-www-form-urlencoded."}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := r.ParseForm(); err != nil {
		return &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_request", Description: "The form could not be read."}
	}
	return nil
}

func (s *Server) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(oauth.MetadataMaxAge.Seconds())))
	respondJSON(w, r, http.StatusOK, s.OAuth.Metadata(APIPrefix))
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	if err := oauthForm(w, r); err != nil {
		respondOAuthError(w, r, err)
		return
	}
	cred, err := oauth.ReadCredentials(r.Header.Get("Authorization"), r.PostForm)
	if err != nil {
		respondOAuthError(w, r, err)
		return
	}
	out, err := s.OAuth.Token(r.Context(), cred, r.PostForm)
	if err != nil {
		respondOAuthError(w, r, err)
		return
	}
	respondOAuth(w, http.StatusOK, out)
}

func (s *Server) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	if err := oauthForm(w, r); err != nil {
		respondOAuthError(w, r, err)
		return
	}
	cred, err := oauth.ReadCredentials(r.Header.Get("Authorization"), r.PostForm)
	if err == nil {
		err = s.OAuth.Revoke(r.Context(), cred, r.PostForm.Get("token"))
	}
	if err != nil {
		respondOAuthError(w, r, err)
		return
	}
	respondOAuth(w, http.StatusOK, nil)
}

func (s *Server) handleListOAuthClients(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	clients, err := s.OAuth.Clients(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"clients": clients})
}

func (s *Server) handleCreateOAuthClient(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	var in oauth.ClientInput
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	client, lsn, err := s.OAuth.CreateClient(r.Context(), in, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"client": client})
}

func oauthClientID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		return uuid.Nil, oauth.ErrNoSuchClient
	}
	return id, nil
}

func (s *Server) handleUpdateOAuthClient(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	id, err := oauthClientID(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	var in oauth.ClientChanges
	if err := decodeJSON(w, r, &in); err != nil {
		respondError(w, r, err)
		return
	}
	client, lsn, err := s.OAuth.UpdateClient(r.Context(), id, in, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"client": client})
}

func (s *Server) handleDeleteOAuthClient(w http.ResponseWriter, r *http.Request) {
	if s.OAuth == nil {
		respondError(w, r, errOAuthOff)
		return
	}
	id, err := oauthClientID(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.OAuth.DeleteClient(r.Context(), id, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
