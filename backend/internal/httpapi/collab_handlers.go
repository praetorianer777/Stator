package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/collab"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

var (
	errCollabOff = &APIError{Status: http.StatusServiceUnavailable, Code: "collab_off",
		Message: "Editing together is not available on this server. Edit on your own; your changes are saved as your draft."}
	errNotUpgrade = &APIError{Status: http.StatusUpgradeRequired, Code: "upgrade_required",
		Message: "This address speaks WebSocket only. Open the page's editor, which connects to it for you."}
	errTooManyEditors = &APIError{Status: http.StatusServiceUnavailable, Code: "too_many_editors",
		Message: "This page has as many people editing it as it can take. Edit on your own for now, or try again in a little while."}
)

// unlessUpgrade leaves a WebSocket out of a middleware, such as the request
// timeout, that would end it as if it were one request.
func unlessUpgrade(mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isUpgrade(r) {
				next.ServeHTTP(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}

// handleCollab opens a page's shared draft as a WebSocket for somebody who
// may edit the page, then hands it to the hub until either side goes.
func (s *Server) handleCollab(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if s.Collab == nil {
		respondError(w, r, errCollabOff)
		return
	}
	if !isUpgrade(r) {
		respondError(w, r, errNotUpgrade)
		return
	}
	actor := actorFrom(r)
	// Counted in before the room is read, so no change sent meanwhile passes it by.
	conn, err := s.Collab.Join(id)
	if err != nil {
		if errors.Is(err, collab.ErrTooMany) {
			respondError(w, r, errTooManyEditors)
		} else {
			respondError(w, r, errCollabOff)
		}
		return
	}
	room, err := s.Pages.OpenCollab(r.Context(), actor, id)
	if err != nil {
		conn.Leave()
		respondError(w, r, err)
		return
	}
	if room.Replaced != nil {
		s.Collab.Reset(id, *room.Replaced)
	}
	// The server's deadlines are for requests; this one lasts as long as
	// somebody edits, and the hub's pings watch over it instead.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	// The origin was held to this site's by sameSite already.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		conn.Leave()
		return
	}
	ws.SetReadLimit(collab.MaxMessageBytes)
	updates := make([]collab.Update, len(room.Updates))
	for i, u := range room.Updates {
		updates[i] = collab.Update{Seq: u.Seq, Body: u.Body}
	}
	store := &collabStore{s: s, actor: actor, page: id, room: room.ID, credential: credentialFrom(r, s.CookieName)}
	conn.Serve(context.WithoutCancel(r.Context()), ws, collab.Room{ID: room.ID, Base: room.Base, Seed: room.Seed, Updates: updates}, store)
}

// collabStore is the page service as the person a connection is for.
type collabStore struct {
	s          *Server
	actor      perm.Actor
	page, room uuid.UUID
	credential string
}

// collabError reads the page service's refusals as the hub's.
func collabError(err error) error {
	var denied *perm.DeniedError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, page.ErrRoomGone), errors.Is(err, page.ErrRoomSeeded):
		return collab.ErrGone
	case errors.Is(err, page.ErrCollabRefused), errors.Is(err, page.ErrNotFound), errors.As(err, &denied):
		return collab.ErrRefused
	case errors.Is(err, page.ErrBadBase), errors.Is(err, page.ErrCompactStale):
		return collab.ErrIgnored
	}
	return err
}

func (c *collabStore) Append(ctx context.Context, body []byte) (int64, error) {
	seq, err := c.s.Pages.AppendCollab(ctx, c.actor, c.page, c.room, body)
	return seq, collabError(err)
}

func (c *collabStore) Seed(ctx context.Context, base int, body []byte) error {
	return collabError(c.s.Pages.SeedCollab(ctx, c.actor, c.page, c.room, base, body))
}

func (c *collabStore) Published(ctx context.Context, version int) (int, error) {
	base, err := c.s.Pages.PublishedCollab(ctx, c.actor, c.page, c.room, version)
	return base, collabError(err)
}

func (c *collabStore) Discard(ctx context.Context) error {
	return collabError(c.s.Pages.DiscardCollab(ctx, c.actor, c.page, c.room))
}

func (c *collabStore) Compact(ctx context.Context, from, to int64, count int, merged []byte) error {
	return collabError(c.s.Pages.CompactCollab(ctx, c.actor, c.page, c.room, from, to, count, merged))
}

func (c *collabStore) Reload(ctx context.Context) (int, []collab.Update, error) {
	base, rows, err := c.s.Pages.ReloadCollab(ctx, c.actor, c.page, c.room)
	if err != nil {
		return 0, nil, collabError(err)
	}
	out := make([]collab.Update, len(rows))
	for i, u := range rows {
		out[i] = collab.Update{Seq: u.Seq, Body: u.Body}
	}
	return base, out, nil
}

func (c *collabStore) Count(ctx context.Context) (int, error) {
	n, err := c.s.Pages.CountCollab(ctx, c.actor, c.room)
	return n, collabError(err)
}

// Check signs the person in again with the credential they came with, so a
// session that ended or a member who left is let go, then asks the page.
func (c *collabStore) Check(ctx context.Context) error {
	if c.s.Auth == nil {
		return collab.ErrSignedOut
	}
	principal, err := c.s.Auth.Authenticate(ctx, c.credential)
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		return collab.ErrSignedOut
	case err != nil:
		return err
	case principal.UserID != c.actor.UserID:
		return collab.ErrSignedOut
	}
	return collabError(c.s.Pages.CheckCollab(ctx, c.actor, c.page))
}
