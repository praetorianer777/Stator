package notify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// FieldError is a refusal of one field of the request.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

// On says whether the switch for a kind is on; a kind it does not know is on.
func (s Switches) On(kind Kind) bool {
	switch kind {
	case KindMentioned:
		return s.Mentioned
	case KindReplied:
		return s.Replied
	case KindCommented:
		return s.Commented
	case KindResolved:
		return s.Resolved
	case KindPublished:
		return s.Published
	case KindExpired:
		return s.Expired
	case KindCreated:
		return s.Created
	}
	return true
}

// allOn is every switch on, what nobody has said otherwise means.
var allOn = Switches{Mentioned: true, Replied: true, Commented: true, Resolved: true, Published: true, Created: true, Expired: true}

// DefaultPreferences is what having saved nothing means.
func DefaultPreferences() Preferences {
	return Preferences{InApp: allOn, Email: allOn, Digest: DigestOff, AutoWatch: true}
}

// switchesFrom reads a stored map, where a missing kind is on, as in Armature.
func switchesFrom(raw []byte) (Switches, error) {
	stored := map[Kind]bool{}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return Switches{}, err
	}
	on := func(k Kind) bool {
		v, set := stored[k]
		return !set || v
	}
	return Switches{
		Mentioned: on(KindMentioned), Replied: on(KindReplied), Commented: on(KindCommented),
		Resolved: on(KindResolved), Published: on(KindPublished), Created: on(KindCreated),
		Expired: on(KindExpired),
	}, nil
}

func (s Switches) stored() map[Kind]bool {
	out := make(map[Kind]bool, len(Kinds))
	for _, k := range Kinds {
		out[k] = s.On(k)
	}
	return out
}

// Validate refuses a digest the product does not know.
func (p Preferences) Validate() error {
	for _, d := range Digests {
		if p.Digest == d {
			return nil
		}
	}
	return &FieldError{Field: "digest", Message: "That is not a digest schedule. Choose off, hourly or daily."}
}

func readPreferences(ctx context.Context, tx db.DBTX, userID uuid.UUID) (Preferences, error) {
	var (
		inApp, email []byte
		p            Preferences
	)
	err := tx.QueryRow(ctx, `SELECT in_app, email, digest, auto_watch FROM notification_preference WHERE user_id = $1`, userID).
		Scan(&inApp, &email, &p.Digest, &p.AutoWatch)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultPreferences(), nil
	}
	if err != nil {
		return p, err
	}
	if p.InApp, err = switchesFrom(inApp); err != nil {
		return p, err
	}
	p.Email, err = switchesFrom(email)
	return p, err
}

// Excerpt cuts words a notification quotes to MaxExcerptLength characters,
// on one line.
func Excerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= MaxExcerptLength {
		return text
	}
	return strings.TrimSpace(string([]rune(text)[:MaxExcerptLength]))
}

// Service reads and marks a person's notifications and keeps their preferences.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// selectNotifications reads rows with the page as it is now. Only the
// recipient's own rows, about pages they may still view, out of the trash.
const selectNotifications = `
SELECT n.id, n.kind, n.actor_id, COALESCE(u.name, ''), p.id, p.title, s.key,
       n.thread_id, n.comment_id, n.version, n.excerpt, n.created_at, n.read_at
FROM notification n
JOIN page p ON p.id = n.page_id
JOIN space s ON s.id = p.space_id
LEFT JOIN app_user u ON u.id = n.actor_id
WHERE n.user_id = $1 AND p.trashed_at IS NULL AND ` + "perm_page_viewable(p.id, $1::uuid)"

func scanNotifications(rows pgx.Rows) ([]Notification, error) {
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var (
			n    Notification
			kind string
		)
		if err := rows.Scan(&n.ID, &kind, &n.ActorID, &n.ActorName, &n.Page.ID, &n.Page.Title, &n.Page.SpaceKey,
			&n.ThreadID, &n.CommentID, &n.Version, &n.Excerpt, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, err
		}
		n.Kind = Kind(kind)
		out = append(out, n)
	}
	return out, rows.Err()
}

// List is what the caller was told, the latest first, and how many the
// filter lists in all.
func (s *Service) List(ctx context.Context, actor perm.Actor, unreadOnly bool, limit, offset int) ([]Notification, int, error) {
	filter := ""
	if unreadOnly {
		filter = ` AND n.read_at IS NULL`
	}
	var (
		out   []Notification
		total int
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (`+selectNotifications+filter+`) listed`, actor.UserID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectNotifications+filter+` ORDER BY n.created_at DESC, n.id DESC LIMIT $2 OFFSET $3`, actor.UserID, limit, offset)
		if err != nil {
			return err
		}
		out, err = scanNotifications(rows)
		return err
	})
	return out, total, err
}

// Unread counts what the caller has not read, for the badge.
func (s *Service) Unread(ctx context.Context, actor perm.Actor) (int, error) {
	var n int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM (`+selectNotifications+` AND n.read_at IS NULL) unread`, actor.UserID).Scan(&n)
	})
	return n, err
}

// MarkRead marks the named rows read, or all of them; rows that are not the
// caller's are left alone.
func (s *Service) MarkRead(ctx context.Context, actor perm.Actor, in MarkReadInput) (db.LSN, error) {
	if !in.All && len(in.IDs) == 0 {
		return 0, &FieldError{Field: "ids", Message: "Name the notifications to mark read, or send all as true."}
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if in.All {
			_, err := tx.Exec(ctx, `UPDATE notification SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, actor.UserID)
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE notification SET read_at = now() WHERE user_id = $1 AND read_at IS NULL AND id = ANY($2)`, actor.UserID, in.IDs)
		return err
	})
}

// Preferences are the caller's, or the defaults when they saved none.
func (s *Service) Preferences(ctx context.Context, actor perm.Actor) (Preferences, error) {
	var out Preferences
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = readPreferences(ctx, tx, actor.UserID)
		return err
	})
	return out, err
}

// SavePreferences replaces the caller's preferences with the ones sent.
func (s *Service) SavePreferences(ctx context.Context, actor perm.Actor, in Preferences) (Preferences, db.LSN, error) {
	if err := in.Validate(); err != nil {
		return Preferences{}, 0, err
	}
	inApp, _ := json.Marshal(in.InApp.stored())
	email, _ := json.Marshal(in.Email.stored())
	var out Preferences
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO notification_preference (org_id, user_id, in_app, email, digest, auto_watch)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (org_id, user_id) DO UPDATE
			SET in_app = EXCLUDED.in_app, email = EXCLUDED.email, digest = EXCLUDED.digest,
			    auto_watch = EXCLUDED.auto_watch, updated_at = now()`,
			actor.UserID, inApp, email, in.Digest, in.AutoWatch); err != nil {
			return err
		}
		var err error
		out, err = readPreferences(ctx, tx, actor.UserID)
		return err
	})
	return out, lsn, err
}
