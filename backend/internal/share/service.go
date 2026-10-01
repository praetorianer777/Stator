package share

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/perm"
)

// brakeConstraint is what the database names when a share passes MaxPerHour.
const brakeConstraint = "page_share_per_hour"

// Service sends pages and answers the share dialog's questions.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// shown is a page as the actor may view it, out of the trash.
type shown struct {
	title, spaceKey string
	published       bool
}

func viewable(ctx context.Context, tx db.DBTX, actor perm.Actor, pageID uuid.UUID) (shown, error) {
	var out shown
	err := tx.QueryRow(ctx, `
		SELECT p.title, s.key, p.version > 0 FROM page p JOIN space s ON s.id = p.space_id
		WHERE p.id = $1 AND p.trashed_at IS NULL AND `+perm.ViewablePage("p", 2), pageID, actor.UserID).Scan(&out.title, &out.spaceKey, &out.published)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrPageNotFound
	}
	return out, err
}

// Recipients are the members and groups matching q as the pickers match
// them, each saying whether, or how many of its members, may view the page.
func (s *Service) Recipients(ctx context.Context, actor perm.Actor, pageID uuid.UUID, q string, limit int) ([]Recipient, []RecipientGroup, error) {
	people, groups := []Recipient{}, []RecipientGroup{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := viewable(ctx, tx, actor, pageID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name, u.email::text, perm_page_viewable($1, u.id)
			FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id() AND perm_global_holds(u.id, 'use')
			  AND (u.name ILIKE $2 OR u.name ILIKE '% ' || $2 OR u.email::text ILIKE $2)
			ORDER BY lower(u.name), u.email LIMIT $3`, pageID, perm.LikePrefix(q), perm.PickerLimit(limit))
		if err != nil {
			return err
		}
		people, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Recipient, error) {
			var p Recipient
			err := row.Scan(&p.ID, &p.Name, &p.Email, &p.CanView)
			return p, err
		})
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `
			SELECT g.id, g.name, count(gm.user_id)::int, g.source = 'oidc',
			       count(gm.user_id) FILTER (WHERE perm_page_viewable($1, gm.user_id))::int
			FROM groups g LEFT JOIN group_member gm ON gm.group_id = g.id
			WHERE g.name ILIKE $2 OR g.name ILIKE '% ' || $2
			GROUP BY g.id
			ORDER BY lower(g.name), g.id LIMIT $3`, pageID, perm.LikePrefix(q), perm.PickerLimit(limit))
		if err != nil {
			return err
		}
		groups, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (RecipientGroup, error) {
			var g RecipientGroup
			err := row.Scan(&g.ID, &g.Name, &g.MemberCount, &g.FromProvider, &g.Viewers)
			return g, err
		})
		return err
	})
	if people == nil {
		people = []Recipient{}
	}
	if groups == nil {
		groups = []RecipientGroup{}
	}
	return people, groups, err
}

// Viewers are the members who may view a page, by name, and how many.
func (s *Service) Viewers(ctx context.Context, actor perm.Actor, pageID uuid.UUID, limit, offset int) (Viewers, error) {
	out := Viewers{People: []perm.Person{}}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := viewable(ctx, tx, actor, pageID); err != nil {
			return err
		}
		var members int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE perm_page_viewable($1, m.user_id))::int,
			       count(*) FILTER (WHERE perm_global_holds(m.user_id, 'use'))::int
			FROM org_member m WHERE m.org_id = current_org_id()`, pageID).Scan(&out.Total, &members); err != nil {
			return err
		}
		out.Everyone = out.Total >= members
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name, u.email::text
			FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id() AND perm_page_viewable($1, u.id)
			ORDER BY lower(u.name), u.email, u.id LIMIT $2 OFFSET $3`, pageID, limit, offset)
		if err != nil {
			return err
		}
		people, err := pgx.CollectRows(rows, pgx.RowToStructByPos[perm.Person])
		if people != nil {
			out.People = people
		}
		return err
	})
	return out, err
}

// Share sends a page to the people and groups named, if every one of them
// may view it; otherwise it sends nothing and says who may not.
func (s *Service) Share(ctx context.Context, actor perm.Actor, pageID uuid.UUID, in Input) (*Share, db.LSN, error) {
	message, err := in.Clean()
	if err != nil {
		return nil, 0, err
	}
	var out Share
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		page, err := viewable(ctx, tx, actor, pageID)
		if err != nil {
			return err
		}
		if !page.published {
			return ErrUnpublished
		}
		subjects, err := perm.ResolveSubjects(ctx, tx, "recipients", in.Recipients, false)
		if err != nil {
			return err
		}
		people, err := resolvePeople(ctx, tx, actor.UserID, pageID, subjects)
		if err != nil {
			return err
		}
		if len(people) == 0 {
			return &FieldError{Field: "recipients", Message: "Pick somebody other than yourself to share the page with."}
		}
		if len(people) > MaxPeople {
			return &FieldError{Field: "recipients", Message: fmt.Sprintf(
				"That would tell %d people. Share with at most %d at once, or pick smaller groups.", len(people), MaxPeople)}
		}
		if err := brake(ctx, tx, actor.UserID); err != nil {
			return err
		}
		out = Share{PageID: pageID, Recipients: subjects, People: len(people), Message: message}
		err = tx.QueryRow(ctx, `
			INSERT INTO page_share (org_id, page_id, sharer_id, message)
			VALUES (current_org_id(), $1, $2, $3)
			RETURNING id, created_at`, pageID, actor.UserID, message).Scan(&out.ID, &out.CreatedAt)
		if err != nil {
			return braked(err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO page_share_recipient (org_id, share_id, user_id)
			SELECT current_org_id(), $1, unnest($2::uuid[])`, out.ID, people); err != nil {
			return fmt.Errorf("name the people told: %w", err)
		}
		if err := events.Emit(ctx, tx, events.TopicPageShared, events.PageShared{ShareID: out.ID, PageID: pageID, ActorID: actor.UserID}); err != nil {
			return err
		}
		// The note is left out: it is a message to the people told, not a change to the organization.
		return perm.Record(ctx, tx, actor, audit.ActionPageShared, "page", &pageID, map[string]any{
			"title": page.title, "space": page.spaceKey, "recipients": perm.SubjectLog(subjects), "people": len(people)})
	})
	if err != nil {
		return nil, lsn, err
	}
	return &out, lsn, nil
}

// resolvePeople is whom a share tells: each person named and each group's members
// who may view, never the sharer. A person or whole group shut out refuses it.
func resolvePeople(ctx context.Context, tx db.DBTX, sharer, pageID uuid.UUID, subjects []perm.Subject) ([]uuid.UUID, error) {
	var (
		people []uuid.UUID
		closed []perm.Subject
	)
	seen := map[uuid.UUID]bool{sharer: true}
	add := func(id uuid.UUID) {
		if !seen[id] {
			seen[id] = true
			people = append(people, id)
		}
	}
	for _, sub := range subjects {
		switch sub.Type {
		case perm.SubjectUser:
			if *sub.ID == sharer {
				continue
			}
			var may bool
			if err := tx.QueryRow(ctx, `SELECT perm_page_viewable($1, $2)`, pageID, *sub.ID).Scan(&may); err != nil {
				return nil, err
			}
			if !may {
				closed = append(closed, sub)
				continue
			}
			add(*sub.ID)
		case perm.SubjectGroup:
			rows, err := tx.Query(ctx, `
				SELECT gm.user_id, perm_page_viewable($1, gm.user_id)
				FROM group_member gm
				WHERE gm.group_id = $2 AND gm.user_id <> $3 AND perm_is_member(gm.user_id)
				ORDER BY gm.user_id`, pageID, *sub.ID, sharer)
			if err != nil {
				return nil, err
			}
			type member struct {
				id  uuid.UUID
				may bool
			}
			members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (member, error) {
				var m member
				err := row.Scan(&m.id, &m.may)
				return m, err
			})
			if err != nil {
				return nil, err
			}
			viewers := 0
			for _, m := range members {
				if m.may {
					viewers++
					add(m.id)
				}
			}
			if len(members) > 0 && viewers == 0 {
				closed = append(closed, sub)
			}
		}
	}
	if len(closed) > 0 {
		return nil, &CannotViewError{Closed: closed}
	}
	return people, nil
}

// brake refuses a share past MaxPerHour with when the next one fits. The
// database holds the same line; this is what words it.
func brake(ctx context.Context, tx db.DBTX, sharer uuid.UUID) error {
	var (
		count  int
		oldest *time.Time
		now    time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT count(*)::int, min(created_at), now() FROM page_share
		WHERE sharer_id = $1 AND created_at > now() - make_interval(secs => $2)`,
		sharer, RateWindow.Seconds()).Scan(&count, &oldest, &now)
	if err != nil {
		return err
	}
	if count >= MaxPerHour && oldest != nil {
		return &RateLimitedError{RetryAfter: RetryAfter(*oldest, now)}
	}
	return nil
}

// braked words the database's own refusal of a share past MaxPerHour, which
// a share racing this one can meet after brake let it through.
func braked(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == brakeConstraint {
		return &RateLimitedError{RetryAfter: RateWindow}
	}
	return err
}
