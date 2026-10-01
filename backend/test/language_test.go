//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// The interface language is the person's: chosen through the API, read back
// with the session, handed back to the browser, and held by the database to
// the languages the interface speaks.
func TestInterfaceLanguage(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "language")
	me := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	locale := func(r response) any { return obj(t, r, "user")["locale"] }

	t.Run("nobody has chosen until they do, so the browser decides", func(t *testing.T) {
		if got := locale(want(t, me.get(t, "/api/v1/auth/me"), http.StatusOK, "read me")); got != "" {
			t.Fatalf("a new account's language is %v, want empty", got)
		}
	})

	t.Run("a choice is answered at once and kept for the next request", func(t *testing.T) {
		if got := locale(want(t, me.patch(t, "/api/v1/auth/me", map[string]any{"locale": "de"}), http.StatusOK, "choose German")); got != "de" {
			t.Fatalf("the answer says %v, want de", got)
		}
		if got := locale(want(t, me.get(t, "/api/v1/auth/me"), http.StatusOK, "read me again")); got != "de" {
			t.Fatalf("the next request says %v, want de", got)
		}
		if got := locale(want(t, me.patch(t, "/api/v1/auth/me", map[string]any{}), http.StatusOK, "change nothing")); got != "de" {
			t.Fatalf("a body without a language changed it to %v", got)
		}
	})

	t.Run("a language the interface does not speak is refused with a sentence", func(t *testing.T) {
		r := want(t, me.patch(t, "/api/v1/auth/me", map[string]any{"locale": "fr"}), http.StatusUnprocessableEntity, "choose French")
		if code := errorCode(t, r); code != "validation_failed" {
			t.Fatalf("code %q", code)
		}
		if got := locale(want(t, me.get(t, "/api/v1/auth/me"), http.StatusOK, "read me")); got != "de" {
			t.Fatalf("a refused choice left %v", got)
		}
	})

	t.Run("an empty choice hands the language back to the browser", func(t *testing.T) {
		if got := locale(want(t, me.patch(t, "/api/v1/auth/me", map[string]any{"locale": ""}), http.StatusOK, "follow the browser")); got != "" {
			t.Fatalf("the answer says %v, want empty", got)
		}
		var stored *string
		if err := h.super.QueryRow(context.Background(), `SELECT locale FROM app_user WHERE id = $1`, home.user).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != nil {
			t.Fatalf("the row holds %q, want null", *stored)
		}
	})

	t.Run("the database refuses a language the service would", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `UPDATE app_user SET locale = 'fr' WHERE id = $1`, home.user)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("storing fr = %v, want a check violation", err)
		}
	})
}
