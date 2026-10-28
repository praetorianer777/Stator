package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/stator/backend/internal/diagram"
	"github.com/praetorianer777/stator/backend/internal/document"
)

// Diagrams drawn by the installation's Kroki (#546): an author's preview by
// its text, and a reader's by the page and the diagram's hash, so what may be
// drawn for a reader is what the page holds.

// DiagramPreviewsPerMinute is the brake on one person's previews; a preview
// follows typing, after a pause, so this is well above what one author asks.
const DiagramPreviewsPerMinute = 120

var diagramPreviews = newThrottle(DiagramPreviewsPerMinute, time.Minute)

// diagramCSP lets a drawing show its own styles and the pictures it embeds,
// and nothing else, even saved and opened on its own.
const diagramCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:"

// diagramMaxAge is how long a reader's browser keeps a drawing; its address
// names its text, so it never changes.
const diagramMaxAge = 24 * time.Hour

var diagramHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

const errDiagramPreviews = "You previewed diagrams too often in the last minute. Wait a minute and try again."

var (
	errDiagramEditors = ErrForbidden("Only people who may edit pages preview diagrams. Ask a space administrator to let you add pages.")
	errNoSuchDiagram  = ErrNotFound("This page has no such diagram. Open the page again to load its diagrams.")
)

// diagramRequest is a diagram to preview: its language and its text.
type diagramRequest struct {
	Language string `json:"language"`
	Source   string `json:"source"`
}

func (s *Server) handlePreviewDiagram(w http.ResponseWriter, r *http.Request) {
	var req diagramRequest
	if err := decodeJSONWithin(w, r, &req, maxBodyBytes); err != nil {
		respondError(w, r, err)
		return
	}
	if s.Perms != nil {
		may, err := s.Perms.MayEditSomewhere(r.Context(), actorFrom(r))
		if err != nil {
			respondError(w, r, err)
			return
		}
		if !may {
			respondError(w, r, errDiagramEditors)
			return
		}
	}
	if !diagramPreviews.gate(w, r, PrincipalFrom(r.Context()).UserID.String(), errDiagramPreviews) {
		return
	}
	svg, err := s.Diagrams.Draw(r.Context(), req.Language, req.Source)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	sendDiagram(w, svg)
}

func (s *Server) handlePageDiagram(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, _, err := s.Pages.Get(r.Context(), actorFrom(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.drawPageDiagram(w, r, got.Body)
}

func (s *Server) handlePublicPageDiagram(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "pageID", "page")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	got, err := s.Public.Page(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	s.drawPageDiagram(w, r, got.Body)
}

func (s *Server) handleLinkedPageDiagram(w http.ResponseWriter, r *http.Request) {
	got, err := s.Public.LinkedPage(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	// The link's middleware has said no-store already.
	s.drawPageDiagram(w, r, got.Body)
}

// drawPageDiagram answers the drawing of the page's diagram the path's hash
// names; a hash the page does not hold is not found.
func (s *Server) drawPageDiagram(w http.ResponseWriter, r *http.Request, body json.RawMessage) {
	hash := chi.URLParam(r, "hash")
	if !diagramHash.MatchString(hash) {
		respondError(w, r, errNoSuchDiagram)
		return
	}
	root, err := document.Parse(body)
	if err != nil {
		respondError(w, r, ErrInternal(err))
		return
	}
	for _, d := range document.Diagrams(root) {
		if !diagram.Drawn(d.Language) || diagram.Hash(d.Language, d.Source) != hash {
			continue
		}
		svg, err := s.Diagrams.Draw(r.Context(), d.Language, d.Source)
		if err != nil {
			respondError(w, r, err)
			return
		}
		sendDiagram(w, svg)
		return
	}
	respondError(w, r, errNoSuchDiagram)
}

// sendDiagram answers a drawing to be shown as an image and never as part of
// a page, so a browser runs nothing in it whatever its language wrote.
func sendDiagram(w http.ResponseWriter, svg []byte) {
	h := w.Header()
	h.Set("Content-Type", "image/svg+xml")
	h.Set("Content-Security-Policy", diagramCSP)
	h.Set("Content-Length", strconv.Itoa(len(svg)))
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "private, max-age="+strconv.Itoa(int(diagramMaxAge.Seconds())))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(svg)
}
