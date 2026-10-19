package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// Connecting one's Armature account with one click (#428): the start and
// the return of Armature's authorization code flow. Both are browser
// navigations, so they answer with redirects, and a refusal lands on the
// profile page with an outcome it can explain.

const (
	// armatureOutcomeParam names how a connect ended, for the profile page.
	armatureOutcomeParam = "armature"
	// armatureReturnParam is the path to go back to afterwards, as the
	// profile page's connect hints name it.
	armatureReturnParam = "return"
	profilePath         = "/settings/profile"
	armatureSectionID   = "armature"
	// maxReturnLength bounds the way back, as the web client does.
	maxReturnLength = 2000
)

// Outcomes the profile page explains.
const (
	connectConnected   = "connected"
	connectDenied      = "denied"
	connectExpired     = "expired"
	connectEnded       = "ended"
	connectRefused     = "refused"
	connectUnavailable = "unavailable"
	connectUnreachable = "unreachable"
	connectOther       = "other_installation"
	connectNoArmature  = "not_configured"
	connectFailed      = "failed"
)

// armatureReturn is the path to land on afterwards, kept inside this
// application by the same rules as a sign-in's.
func armatureReturn(raw string) string {
	if len(raw) > maxReturnLength {
		return ""
	}
	return safeRedirect(raw)
}

func (s *Server) handleStartArmatureConnect(w http.ResponseWriter, r *http.Request) {
	back := armatureReturn(r.URL.Query().Get(armatureReturnParam))
	p := PrincipalFrom(r.Context())
	if s.Armature == nil || p == nil || p.SessionID == nil {
		s.landAfterConnect(w, r, back, connectUnavailable)
		return
	}
	target, err := s.Armature.StartConnect(r.Context(), *p.SessionID, back)
	if err != nil {
		loggerFrom(r.Context()).Info("a connect to Armature could not start", "error", err)
		s.landAfterConnect(w, r, back, connectFailure(err))
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) handleFinishArmatureConnect(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if s.Armature == nil || p == nil || p.SessionID == nil {
		s.landAfterConnect(w, r, "", connectUnavailable)
		return
	}
	back, lsn, err := s.Armature.FinishConnect(r.Context(), *p.SessionID, r.URL.Query())
	noteWrite(r.Context(), lsn)
	if err != nil {
		loggerFrom(r.Context()).Info("a connect to Armature did not finish", "error", err)
		s.landAfterConnect(w, r, armatureReturn(back), connectFailure(err))
		return
	}
	s.landAfterConnect(w, r, armatureReturn(back), connectConnected)
}

// connectFailure names a refusal for the profile page to explain.
func connectFailure(err error) string {
	var refused *armature.AppRefusedError
	switch {
	case errors.As(err, &refused):
		return connectRefused
	case errors.Is(err, armature.ErrNotConfigured):
		return connectNoArmature
	case errors.Is(err, armature.ErrAppUnavailable):
		return connectUnavailable
	case errors.Is(err, armature.ErrOAuthAttempt):
		return connectExpired
	case errors.Is(err, armature.ErrConnectDenied):
		return connectDenied
	case errors.Is(err, armature.ErrConnectEnded):
		return connectEnded
	case errors.Is(err, armature.ErrOtherInstallation):
		return connectOther
	case errors.Is(err, armature.ErrUnreachable):
		return connectUnreachable
	default:
		return connectFailed
	}
}

// landAfterConnect sends the browser to the profile's Armature section,
// which says how the connect ended and, once connected, goes back.
func (s *Server) landAfterConnect(w http.ResponseWriter, r *http.Request, back, outcome string) {
	q := url.Values{armatureOutcomeParam: {outcome}}
	if back != "" {
		q.Set(armatureReturnParam, back)
	}
	http.Redirect(w, r, s.AppBaseURL+profilePath+"?"+q.Encode()+"#"+armatureSectionID, http.StatusFound)
}

func (s *Server) handleSaveArmatureApp(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	var req armature.AppInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	connection, lsn, err := s.Armature.SaveApp(r.Context(), req, clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"connection": connection})
}

func (s *Server) handleRemoveArmatureApp(w http.ResponseWriter, r *http.Request) {
	if s.Armature == nil {
		respondError(w, r, errArmatureOff)
		return
	}
	lsn, err := s.Armature.RemoveApp(r.Context(), clientIP(r))
	noteWrite(r.Context(), lsn)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
