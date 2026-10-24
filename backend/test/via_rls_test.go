//go:build integration

package test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
)

// insertLink puts a link on a page as the acting person, sending a via of its own.
const insertLink = `INSERT INTO page_remote_link (page_id, url, title, source, created_by, via_kind, via_id, via_name)
	VALUES ($1, $2, 'A requirement', 'Exciter', $3, $4, $5, $6) RETURNING id`

// linkVia reads what a link says it was written through.
func linkVia(t *testing.T, h *harness, id uuid.UUID) (kind, name *string) {
	t.Helper()
	if err := h.super.QueryRow(context.Background(), `SELECT via_kind, via_name FROM page_remote_link WHERE id = $1`, id).Scan(&kind, &name); err != nil {
		t.Fatal(err)
	}
	return kind, name
}

// Straight through SQL as stator_app, the database writes what a row came through from the setting alone: a value
// sent by hand is overwritten, none is changed, and somebody else's token names nothing (#498).
func TestRawSQLStampsWhatARowWasWrittenThrough(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "via-sql")
	owner := api.as(t, home.user)
	strangerID := h.addPerson(t, "member")
	docs := newTree(t, owner, "VSQL", "Through SQL")
	plan := docs.add(docs.homeID, "Plan")
	tokenID, _ := makeToken(t, owner, map[string]any{"name": "Nightly script"})
	theirs, _ := makeToken(t, api.as(t, strangerID), map[string]any{"name": "Somebody else's"})
	h.settle(t)

	ctx := context.Background()
	conn := appConn(t)
	actAs(t, conn, home.user)
	forged, script := "oauth", "a forged name"
	n := 0
	write := func(t *testing.T) uuid.UUID {
		t.Helper()
		n++
		var id uuid.UUID
		if err := conn.QueryRow(ctx, insertLink, plan, "https://exciter.example/r/"+uuid.NewString(), home.user, forged, uuid.New(), script).Scan(&id); err != nil {
			t.Fatalf("put link %d: %v", n, err)
		}
		return id
	}
	viaIs := func(t *testing.T, token string) {
		t.Helper()
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.ViaVar, token); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a session's row says nothing, whatever it sends", func(t *testing.T) {
		if kind, name := linkVia(t, h, write(t)); kind != nil || name != nil {
			t.Errorf("a forged via is kept: %v %v", *kind, *name)
		}
	})

	t.Run("a row written through a token names it, and is never changed", func(t *testing.T) {
		viaIs(t, tokenID)
		id := write(t)
		if kind, name := linkVia(t, h, id); kind == nil || *kind != "token" || *name != "Nightly script" {
			t.Errorf("the row is stamped %v %v", kind, name)
		}
		// stator_app holds no privilege on the columns; the trigger holds every role that does.
		refused(t, conn, "stator_app renaming what a row came through", `UPDATE page_remote_link SET via_name = 'renamed' WHERE id = $1`, id)
		refused(t, conn, "stator_app taking off what a row came through",
			`UPDATE page_remote_link SET via_kind = NULL, via_id = NULL, via_name = NULL WHERE id = $1`, id)
		_, err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE page_remote_link SET via_name = 'renamed' WHERE id = $1`, id)
			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "via_fixed" {
			t.Errorf("stator_admin renaming what a row came through = %v, want refused by the trigger", err)
		}
		if _, err := h.super.Exec(ctx, `UPDATE page_remote_link SET via_name = 'renamed' WHERE id = $1`, id); err == nil {
			t.Error("even a superuser may change what a row came through")
		}
		if _, err := conn.Exec(ctx, `UPDATE page_remote_link SET title = 'Retitled' WHERE id = $1`, id); err != nil {
			t.Errorf("the rest of the row cannot be changed: %v", err)
		}
		if kind, name := linkVia(t, h, id); kind == nil || *name != "Nightly script" {
			t.Errorf("a retitled row reads %v %v", kind, name)
		}
	})

	t.Run("the audit log is stamped the same way", func(t *testing.T) {
		viaIs(t, tokenID)
		mark := uuid.NewString()
		if _, err := conn.Exec(ctx, `INSERT INTO audit_log (actor_user_id, action, target_type, via_kind, via_id, via_name, data)
			VALUES ($1, 'token.created', 'api_token', 'oauth', $2, 'a forged name', jsonb_build_object('mark', $3::text))`,
			home.user, uuid.New(), mark); err != nil {
			t.Fatalf("write an audit entry: %v", err)
		}
		var kind, name string
		if err := h.super.QueryRow(ctx, `SELECT via_kind, via_name FROM audit_log WHERE data->>'mark' = $1`, mark).Scan(&kind, &name); err != nil ||
			kind != "token" || name != "Nightly script" {
			t.Errorf("the entry is stamped %q %q (%v)", kind, name, err)
		}
	})

	t.Run("a revoked token keeps its name on what it wrote", func(t *testing.T) {
		viaIs(t, tokenID)
		id := write(t)
		if _, err := h.super.Exec(ctx, `DELETE FROM api_token WHERE id = $1`, tokenID); err != nil {
			t.Fatal(err)
		}
		if _, name := linkVia(t, h, id); name == nil || *name != "Nightly script" {
			t.Errorf("a revoked token's name is lost: %v", name)
		}
		if kind, _ := linkVia(t, h, write(t)); kind != nil {
			t.Errorf("a revoked token still names what is written: %v", *kind)
		}
	})

	t.Run("somebody else's token names nothing", func(t *testing.T) {
		viaIs(t, theirs)
		if kind, _ := linkVia(t, h, write(t)); kind != nil {
			t.Errorf("a token held by somebody else is named: %v", *kind)
		}
	})
}
