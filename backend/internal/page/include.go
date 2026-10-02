package page

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// MaxIncludeDepth is how many includes deep a page is shown: an include in
// an included page is shown, but a chain this long is a mistake, not a page.
const MaxIncludeDepth = 5

var (
	// ErrIncludeCycle refuses an include that leads back to a page on the way to it.
	ErrIncludeCycle = errors.New("this include leads back to a page that includes it, so it is shown only once")
	// ErrIncludeTooDeep refuses an include nested deeper than MaxIncludeDepth.
	ErrIncludeTooDeep = fmt.Errorf("includes are shown %d deep at most; include the page you want directly", MaxIncludeDepth)
)

// IncludedPage names the page an include shows.
type IncludedPage struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	SpaceKey string    `json:"spaceKey"`
}

// IncludedExcerpt names the excerpt an include shows, when it shows one.
type IncludedExcerpt struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Included is what an include shows: the published body of a page, or of one
// of its excerpts, as a document of its own.
type Included struct {
	Page    IncludedPage     `json:"page"`
	Excerpt *IncludedExcerpt `json:"excerpt"`
	Body    json.RawMessage  `json:"body"`
}

// Included reads what an include of a page shows the actor. Via is the
// chain of pages the include sits in, the outermost first: a page on it
// would show itself inside itself. A page the actor may not read, a draft
// never published, a folder and an excerpt no longer on the page are all
// ErrNotFound, so a reader learns nothing of a page kept from them.
func (s *Service) Included(ctx context.Context, actor perm.Actor, id uuid.UUID, excerptID string, via []uuid.UUID) (*Included, error) {
	if slices.Contains(via, id) {
		return nil, ErrIncludeCycle
	}
	if len(via) >= MaxIncludeDepth {
		return nil, ErrIncludeTooDeep
	}
	p, sp, err := s.Get(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if p.Version == 0 || p.Kind == KindFolder || len(p.Body) == 0 {
		return nil, ErrNotFound
	}
	out := &Included{Page: IncludedPage{ID: p.ID, Title: p.Title, SpaceKey: sp.Key}, Body: p.Body}
	if excerptID == "" {
		return out, nil
	}
	root, err := document.Parse(p.Body)
	if err != nil {
		return nil, fmt.Errorf("read the body of page %s: %w", id, err)
	}
	n, ok := document.FindExcerpt(root, excerptID)
	if !ok {
		return nil, ErrNotFound
	}
	name, _ := n.Attrs["name"].(string)
	out.Excerpt = &IncludedExcerpt{ID: excerptID, Name: name}
	if out.Body, err = json.Marshal(document.Node{Type: "doc", Content: n.Content}); err != nil {
		return nil, err
	}
	return out, nil
}
