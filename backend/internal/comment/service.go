package comment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

var (
	// ErrNotFound is also the answer for a comment on a page the caller may
	// not view, and for one whose whole thread is deleted.
	ErrNotFound = errors.New("comment not found")
	// ErrPageNotFound answers a page the caller may not view or that is in the trash.
	ErrPageNotFound = errors.New("page not found")
	// ErrUnpublished refuses a comment on a page nobody but its creator can read yet.
	ErrUnpublished = errors.New("publish the page before commenting on it")
	// ErrNotYours refuses changing somebody else's comment.
	ErrNotYours = errors.New("that comment is somebody else's")
)

// FieldError is a refusal of one field of the request.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// Service keeps the comments on pages. Whether the actor may read or write
// one is decided on its page, and the database's policies hold it to the same.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// onPage is what a comment needs to know of its page for one actor.
type onPage struct {
	id, space uuid.UUID
	published bool
	access    perm.PageAccess
	// deleteAny is the space's delete, which lets its holder delete anybody's comment.
	deleteAny bool
}

func loadPage(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID) (*onPage, error) {
	p := onPage{id: id}
	var version int
	err := tx.QueryRow(ctx, `
		SELECT p.space_id, p.version FROM page p
		WHERE p.id = $1 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 2), id, actor.UserID).Scan(&p.space, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	p.published = version > 0
	if p.access, _, err = perm.ForPage(ctx, tx, actor, id); err != nil {
		return nil, err
	}
	if !p.access.View {
		return nil, ErrPageNotFound
	}
	facts, err := perm.LoadFacts(ctx, tx, actor, p.space)
	if err != nil {
		return nil, err
	}
	p.deleteAny = facts.HoldsSpace(perm.SpaceDelete)
	return &p, nil
}

// canComment is the right to write on a page, published or not.
func (p *onPage) canComment() error {
	if !p.published {
		return ErrUnpublished
	}
	if !p.access.Comment {
		return &perm.DeniedError{Action: perm.AddComments}
	}
	return nil
}

// cleanBody holds a body to the comment allowlist and to saying something.
func cleanBody(body json.RawMessage) (json.RawMessage, error) {
	if len(body) == 0 || string(body) == "null" {
		return nil, &FieldError{Field: "body", Message: "Write something before you post the comment."}
	}
	root, err := document.ParseComment(body)
	if err != nil {
		return nil, &FieldError{Field: "body", Message: err.Error()}
	}
	if strings.TrimSpace(document.PlainText(root)) == "" {
		return nil, &FieldError{Field: "body", Message: "Write something before you post the comment."}
	}
	return body, nil
}

const selectComments = `
SELECT t.id, t.page_id, t.kind, c.id, c.author_id, COALESCE(u.name, ''), c.body, c.deleted_at IS NOT NULL, c.created_at, c.edited_at
FROM comment_thread t
JOIN comment c ON c.org_id = t.org_id AND c.thread_id = t.id
LEFT JOIN app_user u ON u.id = c.author_id`

// threads reads a page's threads in order, leaving out those wholly deleted,
// and says what the actor may do to each.
func threads(ctx context.Context, tx db.DBTX, actor perm.Actor, p *onPage, where string, args ...any) ([]Thread, error) {
	rows, err := tx.Query(ctx, selectComments+` WHERE `+where+` ORDER BY t.created_at, t.id, c.created_at, c.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read the comments: %w", err)
	}
	defer rows.Close()
	out := []Thread{}
	for rows.Next() {
		var (
			threadID, pageID uuid.UUID
			kind             Kind
			c                Comment
			body             []byte
		)
		if err := rows.Scan(&threadID, &pageID, &kind, &c.ID, &c.AuthorID, &c.AuthorName, &body, &c.Deleted, &c.CreatedAt, &c.EditedAt); err != nil {
			return nil, err
		}
		c.ThreadID = threadID
		if body != nil {
			c.Body = body
		}
		mine := c.AuthorID != nil && *c.AuthorID == actor.UserID
		c.Can = CommentCan{
			Edit:   !c.Deleted && mine && p.access.Comment,
			Delete: !c.Deleted && (mine || p.deleteAny),
		}
		if n := len(out); n == 0 || out[n-1].ID != threadID {
			out = append(out, Thread{
				ID: threadID, PageID: pageID, Kind: kind, Comments: []Comment{},
				Can: ThreadCan{Reply: p.published && p.access.Comment},
			})
		}
		last := &out[len(out)-1]
		last.Comments = append(last.Comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	live := out[:0]
	for _, t := range out {
		for _, c := range t.Comments {
			if !c.Deleted {
				live = append(live, t)
				break
			}
		}
	}
	return live, nil
}

// List is a page's threads, oldest first: of one kind, or both when kind is empty.
func (s *Service) List(ctx context.Context, actor perm.Actor, pageID uuid.UUID, kind Kind) ([]Thread, error) {
	var out []Thread
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := loadPage(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		if kind == "" {
			out, err = threads(ctx, tx, actor, p, `t.page_id = $1`, pageID)
		} else {
			out, err = threads(ctx, tx, actor, p, `t.page_id = $1 AND t.kind = $2`, pageID, kind)
		}
		return err
	})
	return out, err
}

// held is one comment's place, read before acting on it.
type held struct {
	thread, page uuid.UUID
	author       *uuid.UUID
	deleted      bool
}

// find reads a comment the actor may see; lock takes it for writing.
func find(ctx context.Context, tx db.DBTX, id uuid.UUID, lock bool) (*held, error) {
	sql := `SELECT thread_id, page_id, author_id, deleted_at IS NOT NULL FROM comment WHERE id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	var h held
	err := tx.QueryRow(ctx, sql, id).Scan(&h.thread, &h.page, &h.author, &h.deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &h, err
}

// thread reads one thread, refusing one wholly deleted.
func thread(ctx context.Context, tx db.DBTX, actor perm.Actor, p *onPage, id uuid.UUID) (*Thread, error) {
	found, err := threads(ctx, tx, actor, p, `t.id = $1`, id)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	return &found[0], nil
}

// locate finds a comment and its page as the actor sees them; a comment on a
// page they may not view, or in the trash, is not found.
func locate(ctx context.Context, tx db.DBTX, actor perm.Actor, id uuid.UUID, lock bool) (*held, *onPage, error) {
	h, err := find(ctx, tx, id, lock)
	if err != nil {
		return nil, nil, err
	}
	p, err := loadPage(ctx, tx, actor, h.page)
	if errors.Is(err, ErrPageNotFound) {
		return nil, nil, ErrNotFound
	}
	return h, p, err
}

// Get is the whole thread a comment belongs to.
func (s *Service) Get(ctx context.Context, actor perm.Actor, commentID uuid.UUID) (*Thread, error) {
	var out *Thread
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, p, err := locate(ctx, tx, actor, commentID, false)
		if err != nil {
			return err
		}
		out, err = thread(ctx, tx, actor, p, h.thread)
		return err
	})
	return out, err
}

// add writes a comment in the actor's name and tells the outbox.
func add(ctx context.Context, tx db.DBTX, actor perm.Actor, id, threadID, pageID uuid.UUID, body json.RawMessage, reply bool) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO comment (org_id, id, thread_id, page_id, author_id, body)
		VALUES (current_org_id(), $1, $2, $3, $4, $5)`, id, threadID, pageID, actor.UserID, []byte(body)); err != nil {
		return fmt.Errorf("add the comment: %w", err)
	}
	return events.Emit(ctx, tx, events.TopicCommentCreated, events.CommentCreated{
		CommentID: id, ThreadID: threadID, PageID: pageID, ActorID: actor.UserID, Reply: reply, Mentioned: []uuid.UUID{},
	})
}

// Start begins a thread below a published page.
func (s *Service) Start(ctx context.Context, actor perm.Actor, pageID uuid.UUID, in ThreadInput) (*Thread, db.LSN, error) {
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, 0, err
	}
	var out *Thread
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		p, err := loadPage(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		if err := p.canComment(); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO comment_thread (org_id, page_id, kind, created_by)
			VALUES (current_org_id(), $1, $2, $3) RETURNING id`, pageID, KindPage, actor.UserID).Scan(&id); err != nil {
			return fmt.Errorf("start the thread: %w", err)
		}
		if err := add(ctx, tx, actor, id, id, pageID, body, false); err != nil {
			return err
		}
		out, err = thread(ctx, tx, actor, p, id)
		return err
	})
	return out, lsn, err
}

// Reply adds a comment at the end of the thread the given comment is in.
func (s *Service) Reply(ctx context.Context, actor perm.Actor, commentID uuid.UUID, in BodyInput) (*Comment, *Thread, db.LSN, error) {
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, nil, 0, err
	}
	var (
		made *Comment
		out  *Thread
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, p, err := locate(ctx, tx, actor, commentID, false)
		if err != nil {
			return err
		}
		if _, err := thread(ctx, tx, actor, p, h.thread); err != nil {
			return err
		}
		if err := p.canComment(); err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := add(ctx, tx, actor, id, h.thread, h.page, body, true); err != nil {
			return err
		}
		if out, err = thread(ctx, tx, actor, p, h.thread); err != nil {
			return err
		}
		for i := range out.Comments {
			if out.Comments[i].ID == id {
				made = &out.Comments[i]
			}
		}
		return nil
	})
	return made, out, lsn, err
}

// Edit rewrites the actor's own comment. Nobody rewrites another person's
// words, not even an administrator, as in Armature.
func (s *Service) Edit(ctx context.Context, actor perm.Actor, commentID uuid.UUID, in BodyInput) (*Comment, db.LSN, error) {
	body, err := cleanBody(in.Body)
	if err != nil {
		return nil, 0, err
	}
	var out *Comment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, p, err := locate(ctx, tx, actor, commentID, true)
		if err != nil {
			return err
		}
		if h.deleted {
			return ErrNotFound
		}
		if h.author == nil || *h.author != actor.UserID {
			return ErrNotYours
		}
		if err := p.canComment(); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE comment SET body = $2, edited_at = now() WHERE id = $1`, commentID, []byte(body)); err != nil {
			return fmt.Errorf("change the comment: %w", err)
		}
		t, err := thread(ctx, tx, actor, p, h.thread)
		if err != nil {
			return err
		}
		for i := range t.Comments {
			if t.Comments[i].ID == commentID {
				out = &t.Comments[i]
			}
		}
		return nil
	})
	return out, lsn, err
}

// Delete takes a comment's words away and leaves its place in the thread.
// Its author may while they may view the page; the space's delete may for
// anybody's, and that is written to the audit log. A deleted one is no change.
func (s *Service) Delete(ctx context.Context, actor perm.Actor, commentID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		h, p, err := locate(ctx, tx, actor, commentID, true)
		if err != nil {
			return err
		}
		mine := h.author != nil && *h.author == actor.UserID
		if !mine && !p.deleteAny {
			return ErrNotYours
		}
		if h.deleted {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE comment SET body = NULL, deleted_at = now(), deleted_by = $2 WHERE id = $1`, commentID, actor.UserID); err != nil {
			return fmt.Errorf("delete the comment: %w", err)
		}
		if mine {
			return nil
		}
		org, err := tenant.MustFromContext(ctx)
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, org.ID, audit.Entry{
			Action: audit.ActionCommentDeleted, TargetType: "comment", TargetID: &commentID, Actor: actor.UserID,
			Data: map[string]any{"pageId": h.page, "threadId": h.thread, "authorId": h.author},
		})
	})
}

// CountsOf is a page's comments as its header shows them; inline and
// detached threads are #23's.
func CountsOf(ctx context.Context, tx db.DBTX, pageID uuid.UUID) (Counts, error) {
	var c Counts
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM comment c JOIN comment_thread t ON t.org_id = c.org_id AND t.id = c.thread_id
		WHERE c.page_id = $1 AND c.deleted_at IS NULL AND t.kind = 'page'`, pageID).Scan(&c.Page)
	return c, err
}
