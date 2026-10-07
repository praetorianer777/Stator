package page

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/keyset"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/rank"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/watch"
)

const (
	// DefaultPostLimit is how many posts a blog shows at once.
	DefaultPostLimit = 10
	// MaxPostExcerptLength bounds the opening words a list quotes of a post.
	MaxPostExcerptLength = 280
	// MaxUnpublishedPosts bounds the caller's own unpublished posts a blog lists.
	MaxUnpublishedPosts = 50
	// FirstPostYear and LastPostYear bound a year the blog is asked for.
	FirstPostYear = 1970
	LastPostYear  = 9999
)

// Post is a blog post as a list shows it: its date, who wrote it and how it begins.
type Post struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	SpaceKey  string    `json:"spaceKey"`
	SpaceName string    `json:"spaceName"`
	// PostedAt is the post's date, when it was first published.
	PostedAt time.Time `json:"postedAt"`
	// AuthorName is who published its first version.
	AuthorName string `json:"authorName"`
	// Excerpt is the post's opening words as plain text.
	Excerpt string  `json:"excerpt"`
	Icon    *string `json:"icon"`
}

// BlogMonth is how many posts a reader may read went out in one month, in UTC.
type BlogMonth struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Count int `json:"count"`
}

// UnpublishedPost is one of the caller's own posts that has not gone out yet.
type UnpublishedPost struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
	// PublishAt is when the caller scheduled it to go out, null when they did not.
	PublishAt *time.Time `json:"publishAt"`
}

// Blog is what a space's blog shows beside its posts: the months that have
// some, the caller's own posts still to go out, and what the caller may do.
type Blog struct {
	SpaceKey  string      `json:"spaceKey"`
	SpaceName string      `json:"spaceName"`
	Months    []BlogMonth `json:"months"`
	// Unpublished are the caller's own posts, newest edit first.
	Unpublished []UnpublishedPost `json:"unpublished"`
	// Watching says whether the caller hears of every new post.
	Watching bool `json:"watching"`
	// CanPost says whether the caller may write a post here.
	CanPost bool `json:"canPost"`
}

// PostsInput narrows a list of posts; every part is optional.
type PostsInput struct {
	// SpaceKey keeps one space's blog; empty is every space not archived.
	SpaceKey string
	// Year keeps the posts of one year, and Month, which needs a year, of
	// one month of it, both in UTC.
	Year, Month int
	After       *keyset.Cursor
	Limit       int
}

// PostInput is a new post.
type PostInput struct {
	Title string `json:"title"`
	// Body is the first document; empty starts with an empty one.
	Body json.RawMessage `json:"body,omitempty"`
	// Template starts the post from a template's key in place of a body; the
	// server fills its variables, and the title's names in braces, from Values.
	Template string            `json:"template,omitempty"`
	Values   map[string]string `json:"values,omitempty"`
	// Publish sends it out at once, dated now; otherwise it stays an
	// unpublished post of its writer's until they publish or schedule it.
	Publish bool `json:"publish,omitempty"`
}

// MonthRange is the stretch of time a year, or a month of it, covers in UTC.
func MonthRange(year, month int) (time.Time, time.Time, error) {
	if month != 0 && year == 0 {
		return time.Time{}, time.Time{}, &FieldError{Field: "month", Message: "Choose a year along with the month."}
	}
	if year != 0 && (year < FirstPostYear || year > LastPostYear) {
		return time.Time{}, time.Time{}, &FieldError{Field: "year", Message: fmt.Sprintf("Choose a year from %d to %d.", FirstPostYear, LastPostYear)}
	}
	if month < 0 || month > 12 {
		return time.Time{}, time.Time{}, &FieldError{Field: "month", Message: "Choose a month from 1 to 12."}
	}
	if year == 0 {
		return time.Time{}, time.Time{}, nil
	}
	if month == 0 {
		from := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
		return from, from.AddDate(1, 0, 0), nil
	}
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, 0), nil
}

// PostExcerpt is a post's opening words on one line, cut after a whole word
// within MaxPostExcerptLength characters.
func PostExcerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= MaxPostExcerptLength {
		return text
	}
	cut := string([]rune(text)[:MaxPostExcerptLength])
	if i := strings.LastIndexFunc(cut, unicode.IsSpace); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRightFunc(cut, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) })
}

// Posts lists the published posts the actor may read, newest first, in one
// blog or across the organization, the trash and the archive left out; next
// is the cursor for the window after, nil at the end.
func (s *Service) Posts(ctx context.Context, actor perm.Actor, in PostsInput) ([]Post, *string, error) {
	if in.Limit < 1 || in.Limit > document.MaxListedPages {
		return nil, nil, &FieldError{Field: "limit", Message: fmt.Sprintf("List 1 to %d posts.", document.MaxListedPages)}
	}
	from, to, err := MonthRange(in.Year, in.Month)
	if err != nil {
		return nil, nil, err
	}
	var (
		out  = []Post{}
		next *string
	)
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var spaceID *uuid.UUID
		if key := strings.TrimSpace(in.SpaceKey); key != "" {
			sp, err := space.Load(ctx, tx, actor, space.ByKey, key)
			if err != nil {
				return err
			}
			spaceID = &sp.ID
		}
		var fromArg, toArg *time.Time
		if !from.IsZero() {
			fromArg, toArg = &from, &to
		}
		afterAt, afterID := in.After.Args()
		rows, err := tx.Query(ctx, `
			SELECT p.id, p.title, s.key, s.name, p.posted_at, COALESCE(u.name, ''),
			       left(COALESCE(page_plain_text(p.body), ''), $8), p.icon
			FROM page p
			JOIN space s ON s.id = p.space_id
			LEFT JOIN page_version v ON v.org_id = p.org_id AND v.page_id = p.id AND v.number = 1
			LEFT JOIN app_user u ON u.id = v.created_by
			WHERE p.kind = 'post' AND p.posted_at IS NOT NULL AND`+live+` AND p.archived_at IS NULL
			  AND CASE WHEN $2::uuid IS NULL THEN s.archived_at IS NULL ELSE p.space_id = $2 END
			  AND ($3::timestamptz IS NULL OR (p.posted_at >= $3 AND p.posted_at < $4))
			  AND ($5::timestamptz IS NULL OR (p.posted_at, p.id) < ($5, $6))
			  AND `+perm.ViewablePage("p", 1)+`
			ORDER BY p.posted_at DESC, p.id DESC
			LIMIT $7`, actor.UserID, spaceID, fromArg, toArg, afterAt, afterID, in.Limit+1, MaxPostExcerptLength*2)
		if err != nil {
			return fmt.Errorf("list the posts: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var p Post
			if err := rows.Scan(&p.ID, &p.Title, &p.SpaceKey, &p.SpaceName, &p.PostedAt, &p.AuthorName, &p.Excerpt, &p.Icon); err != nil {
				return err
			}
			p.Excerpt = PostExcerpt(p.Excerpt)
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	if len(out) > in.Limit {
		out = out[:in.Limit]
		last := out[len(out)-1]
		next = keyset.Next(in.Limit, in.Limit+1, keyset.Cursor{At: last.PostedAt, ID: last.ID})
	}
	return out, next, nil
}

// Blog is a space's blog as its reader finds it: the months with posts they
// may read, newest first, and their own posts still to go out.
func (s *Service) Blog(ctx context.Context, actor perm.Actor, spaceKey string) (*Blog, error) {
	var out *Blog
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		out = &Blog{SpaceKey: sp.Key, SpaceName: sp.Name, CanPost: sp.Can.EditPages}
		rows, err := tx.Query(ctx, `
			SELECT extract(year FROM p.posted_at AT TIME ZONE 'UTC')::int AS y,
			       extract(month FROM p.posted_at AT TIME ZONE 'UTC')::int AS m, count(*)::int
			FROM page p
			WHERE p.space_id = $1 AND p.kind = 'post' AND p.posted_at IS NOT NULL AND`+live+` AND p.archived_at IS NULL
			  AND `+perm.ViewablePage("p", 2)+`
			GROUP BY y, m
			ORDER BY y DESC, m DESC`, sp.ID, actor.UserID)
		if err != nil {
			return fmt.Errorf("count the blog's posts: %w", err)
		}
		if out.Months, err = pgx.CollectRows(rows, pgx.RowToStructByPos[BlogMonth]); err != nil {
			return err
		}
		if out.Months == nil {
			out.Months = []BlogMonth{}
		}
		rows, err = tx.Query(ctx, `
			SELECT p.id, p.title, p.updated_at, sch.publish_at
			FROM page p
			LEFT JOIN page_schedule sch ON sch.page_id = p.id AND sch.user_id = $2 AND sch.failed_at IS NULL
			WHERE p.space_id = $1 AND p.kind = 'post' AND p.version = 0 AND p.created_by = $2 AND`+live+`
			ORDER BY p.updated_at DESC, p.id DESC
			LIMIT $3`, sp.ID, actor.UserID, MaxUnpublishedPosts)
		if err != nil {
			return fmt.Errorf("list the unpublished posts: %w", err)
		}
		if out.Unpublished, err = pgx.CollectRows(rows, pgx.RowToStructByPos[UnpublishedPost]); err != nil {
			return err
		}
		if out.Unpublished == nil {
			out.Unpublished = []UnpublishedPost{}
		}
		out.Watching, err = watch.BlogWatching(ctx, tx, actor.UserID, sp.ID)
		return err
	})
	return out, err
}

// CreatePost writes a new post in a space's blog, unpublished and its
// writer's alone unless it is published at once.
func (s *Service) CreatePost(ctx context.Context, actor perm.Actor, spaceKey string, in PostInput) (*Page, db.LSN, error) {
	title, err := startOf(in.Title, in.Body, in.Template, in.Values)
	if err != nil {
		return nil, 0, err
	}
	r, err := rank.Between("", "")
	if err != nil {
		return nil, 0, err
	}
	var out *Page
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		sp, err := space.Load(ctx, tx, actor, space.ByKey, spaceKey)
		if err != nil {
			return err
		}
		if sp.ArchivedAt != nil {
			return perm.Refuse(perm.EditPages, perm.ArchivedSpace)
		}
		if !sp.Can.EditPages {
			return perm.Refuse(perm.EditPages, "")
		}
		if in.Template != "" {
			if title, in.Body, err = fromTemplate(ctx, tx, sp.ID, in.Template, in.Values, in.Title); err != nil {
				return err
			}
		}
		// Made here rather than returned, which the policies would refuse:
		// the statement's own snapshot does not hold the row it writes.
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `
			INSERT INTO page (id, org_id, space_id, parent_id, rank, title, body, created_by, updated_by, kind, version)
			VALUES ($1, current_org_id(), $2, NULL, $3, $4, COALESCE($5::jsonb, '{"type":"doc","content":[{"type":"paragraph"}]}'::jsonb), $6, $6, 'post', 0)`,
			id, sp.ID, r, title, nullJSON(in.Body), actor.UserID); err != nil {
			return fmt.Errorf("save the post: %w", err)
		}
		if err := watch.Auto(ctx, tx, actor.UserID, id); err != nil {
			return fmt.Errorf("watch the post: %w", err)
		}
		if in.Publish {
			made, _, err := load(ctx, tx, actor, id, true)
			if err != nil {
				return err
			}
			if _, err := publish(ctx, tx, actor, made, release{title: made.Title, body: made.Body, notify: true}); err != nil {
				return err
			}
		}
		out, _, err = load(ctx, tx, actor, id, false)
		return err
	})
	return out, lsn, err
}
