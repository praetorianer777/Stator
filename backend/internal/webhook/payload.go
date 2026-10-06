package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Envelope is what a receiver gets, Armature's shape: the id is the event's,
// the same on every attempt, so a receiver can tell a repeat.
type Envelope struct {
	ID         uuid.UUID `json:"id"`
	Topic      string    `json:"topic"`
	OrgID      uuid.UUID `json:"orgId"`
	OccurredAt time.Time `json:"occurredAt"`
	Payload    any       `json:"payload"`
}

// SpaceRef names a space.
type SpaceRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// PageRef names a page as its reader finds it.
type PageRef struct {
	ID      uuid.UUID `json:"id"`
	Title   string    `json:"title"`
	Version int       `json:"version"`
	URL     string    `json:"url"`
	Space   SpaceRef  `json:"space"`
	// Kind is page, or post for a blog post, whose version 1 is its going out.
	Kind string `json:"kind"`
}

// Person is who acted; null when they are no longer in the organization.
type Person struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Place is where a moved page stood or stands: what the owner may not view
// of it is null.
type Place struct {
	Space    *SpaceRef  `json:"space"`
	ParentID *uuid.UUID `json:"parentId"`
}

// PagePublished is the payload of page.published.
type PagePublished struct {
	Page  PageRef `json:"page"`
	Actor *Person `json:"actor"`
}

// PageMoved is the payload of page.moved.
type PageMoved struct {
	Page  PageRef `json:"page"`
	From  Place   `json:"from"`
	To    Place   `json:"to"`
	Actor *Person `json:"actor"`
}

// PageDeleted is the payload of page.deleted: the page as it went to the trash.
type PageDeleted struct {
	Page  PageRef `json:"page"`
	Actor *Person `json:"actor"`
}

// CommentRef is a comment's words, at most MaxCommentText characters of them.
type CommentRef struct {
	ID       uuid.UUID `json:"id"`
	ThreadID uuid.UUID `json:"threadId"`
	Reply    bool      `json:"reply"`
	Text     string    `json:"text"`
	URL      string    `json:"url"`
}

// CommentCreated is the payload of comment.created.
type CommentCreated struct {
	Comment CommentRef `json:"comment"`
	Page    PageRef    `json:"page"`
	Actor   *Person    `json:"actor"`
}

// Ping is the payload of a test delivery.
type Ping struct {
	Message string `json:"message"`
}

// pingMessage is what a test delivery says.
const pingMessage = "Stator can reach this address."

// body is the envelope for one attempt, its payload read as the endpoint's
// owner through the database's own rules: what they may not view is
// errWithheld, and no part of it is sent.
func (s *Service) body(ctx context.Context, c claimed) ([]byte, error) {
	env := Envelope{ID: c.eventID, Topic: c.topic, OrgID: c.orgID, OccurredAt: c.occurred.UTC()}
	if c.topic == TopicPing {
		env.Payload = Ping{Message: pingMessage}
		return json.Marshal(env)
	}
	if c.owner == nil {
		return nil, errWithheld
	}
	asOwner := db.PinPrimary(db.WithUser(tenant.WithOrg(ctx, tenant.Org{ID: c.orgID}), *c.owner))
	err := s.db.Read(asOwner, func(ctx context.Context, tx db.DBTX) error {
		var err error
		env.Payload, err = s.payload(ctx, tx, c.topic, c.event)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errWithheld
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(env)
}

func (s *Service) payload(ctx context.Context, tx db.DBTX, topic string, event json.RawMessage) (any, error) {
	switch topic {
	case events.TopicPagePublished:
		var in events.PagePublished
		if err := json.Unmarshal(event, &in); err != nil {
			return nil, err
		}
		p, err := s.page(ctx, tx, in.PageID)
		if err != nil {
			return nil, err
		}
		return PagePublished{Page: *p, Actor: person(ctx, tx, in.ActorID)}, nil
	case events.TopicPageMoved:
		var in events.PageMoved
		if err := json.Unmarshal(event, &in); err != nil {
			return nil, err
		}
		p, err := s.page(ctx, tx, in.PageID)
		if err != nil {
			return nil, err
		}
		return PageMoved{Page: *p, From: place(ctx, tx, in.FromSpaceID, in.FromParentID), To: place(ctx, tx, in.ToSpaceID, in.ToParentID),
			Actor: person(ctx, tx, in.ActorID)}, nil
	case events.TopicPageDeleted:
		var in events.PageDeleted
		if err := json.Unmarshal(event, &in); err != nil {
			return nil, err
		}
		p, err := s.page(ctx, tx, in.PageID)
		if err != nil {
			return nil, err
		}
		return PageDeleted{Page: *p, Actor: person(ctx, tx, in.ActorID)}, nil
	case events.TopicCommentCreated:
		var in events.CommentCreated
		if err := json.Unmarshal(event, &in); err != nil {
			return nil, err
		}
		p, err := s.page(ctx, tx, in.PageID)
		if err != nil {
			return nil, err
		}
		c := CommentRef{Reply: in.Reply}
		if err := tx.QueryRow(ctx, `
			SELECT c.id, c.thread_id, left(COALESCE(page_plain_text(c.body), ''), $3)
			FROM comment c WHERE c.id = $1 AND c.page_id = $2 AND c.deleted_at IS NULL`,
			in.CommentID, in.PageID, MaxCommentText).Scan(&c.ID, &c.ThreadID, &c.Text); err != nil {
			return nil, err
		}
		c.URL = p.URL + "?thread=" + c.ThreadID.String()
		return CommentCreated{Comment: c, Page: *p, Actor: person(ctx, tx, in.ActorID)}, nil
	}
	return nil, errWithheld
}

// page reads a published page as the owner; one they may not view is
// pgx.ErrNoRows, as one that is gone is.
func (s *Service) page(ctx context.Context, tx db.DBTX, id uuid.UUID) (*PageRef, error) {
	var p PageRef
	err := tx.QueryRow(ctx, `
		SELECT p.id, p.title, p.version, s.key, s.name, p.kind
		FROM page p JOIN space s ON s.org_id = p.org_id AND s.id = p.space_id
		WHERE p.id = $1 AND p.version > 0`, id).Scan(&p.ID, &p.Title, &p.Version, &p.Space.Key, &p.Space.Name, &p.Kind)
	if err != nil {
		return nil, err
	}
	p.URL = s.opts.AppURL + "/s/" + p.Space.Key + "/p/" + p.ID.String()
	return &p, nil
}

func person(ctx context.Context, tx db.DBTX, id uuid.UUID) *Person {
	p := Person{}
	if err := tx.QueryRow(ctx, `SELECT id, name FROM app_user WHERE id = $1`, id).Scan(&p.ID, &p.Name); err != nil {
		return nil
	}
	return &p
}

// place is a space and a parent, each null unless the owner may view it.
func place(ctx context.Context, tx db.DBTX, spaceID uuid.UUID, parentID *uuid.UUID) Place {
	var out Place
	var sp SpaceRef
	if err := tx.QueryRow(ctx, `SELECT key, name FROM space WHERE id = $1`, spaceID).Scan(&sp.Key, &sp.Name); err == nil {
		out.Space = &sp
	}
	if parentID != nil {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM page WHERE id = $1`, *parentID).Scan(&id); err == nil {
			out.ParentID = &id
		}
	}
	return out
}
