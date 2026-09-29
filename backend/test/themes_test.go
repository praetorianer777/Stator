//go:build integration

package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/tenant"
	"github.com/praetorianer777/stator/backend/internal/theme"
)

func themeObject(org uuid.UUID, themeID, assetID string) string {
	return theme.ObjectKey(org, uuid.MustParse(themeID), uuid.MustParse(assetID))
}

// A theme is its maker's, shared when they say so, chosen per person, and
// walled by the tenant like everything else.
func TestThemesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	home := h.makeMember(t, "themes")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	other := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	admin := api.as(t, h.addPerson(t, home.org, "admin"), home.org, slug)
	away := h.makeMember(t, "themes-away")
	stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))

	spec := map[string]any{
		"colors": map[string]any{"light": map[string]string{"accent": "#ff0066"}, "dark": map[string]string{"accent": "#00ffcc"}},
		"shape":  map[string]any{"radiusControl": 2},
		"css":    "[data-rail] { opacity: .95 }",
	}
	made := want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Magenta", "spec": spec}), http.StatusCreated, "make a theme")
	theme := obj(t, made, "theme")
	themeID := theme["id"].(string)
	if theme["shared"] != false || theme["active"] != false || theme["inUse"].(float64) != 0 || theme["ownerId"] != home.user.String() {
		t.Fatalf("a new theme is the maker's, private, unchosen and unused: %s", made.Raw)
	}
	if obj(t, made, "theme", "spec", "colors", "light")["accent"] != "#ff0066" {
		t.Fatalf("the spec did not come back: %s", made.Raw)
	}

	t.Run("the shipped examples are offered, and one saves as a theme of one's own", func(t *testing.T) {
		listed := want(t, owner.get(t, "/api/v1/themes/examples"), http.StatusOK, "examples")
		examples := list(t, listed, "examples")
		if len(examples) != 2 {
			t.Fatalf("want Deep-Tech and Constellation: %s", listed.Raw)
		}
		first, second := examples[0].(map[string]any), examples[1].(map[string]any)
		if first["key"] != "deep-tech" || first["name"] != "Deep-Tech" || second["key"] != "constellation" {
			t.Fatalf("the examples are %v and %v", first["name"], second["name"])
		}
		saved := want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "My Constellation", "spec": second["spec"]}), http.StatusCreated, "save an example")
		got := obj(t, saved, "theme", "spec")
		if got["effect"] != "constellation" || obj(t, saved, "theme", "spec", "colors", "dark")["accent"] != "#4fd1c5" {
			t.Fatalf("the example did not survive saving: %s", saved.Raw)
		}
		want(t, owner.delete(t, "/api/v1/themes/"+obj(t, saved, "theme")["id"].(string)), http.StatusNoContent, "tidy up")
	})

	t.Run("what is refused on the way in", func(t *testing.T) {
		refuse := func(what string, got response, status int, code string) {
			t.Helper()
			if got.Status != status {
				t.Errorf("%s: got %d, want %d: %s", what, got.Status, status, got.Raw)
				return
			}
			if c := errorCode(t, got); c != code {
				t.Errorf("%s: code %q, want %q", what, c, code)
			}
		}
		refuse("no name", owner.post(t, "/api/v1/themes", map[string]any{"spec": spec}), http.StatusUnprocessableEntity, "validation_failed")
		refuse("the same name twice", owner.post(t, "/api/v1/themes", map[string]any{"name": "magenta"}), http.StatusConflict, "conflict")
		refuse("a colour that is a word", owner.post(t, "/api/v1/themes", map[string]any{"name": "Words", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "red"}}}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse("css that imports", owner.post(t, "/api/v1/themes", map[string]any{"name": "Importer", "spec": map[string]any{"css": "@import url(http://x)"}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse("an unknown field", owner.post(t, "/api/v1/themes", map[string]any{"name": "Typo", "colours": map[string]any{}}), http.StatusBadRequest, "bad_request")
		refuse("css that loads elsewhere", owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"css": ".x{background:url(https://evil/p.png)}"}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse("a file the theme does not have", owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"backdrop": map[string]any{"assetId": uuid.NewString(), "fit": "cover"}}}), http.StatusUnprocessableEntity, "validation_failed")
		refuse("choosing a theme that is not there", owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": uuid.NewString()}), http.StatusNotFound, "not_found")
		refuse("a theme that is not there", owner.get(t, "/api/v1/themes/"+uuid.NewString()), http.StatusNotFound, "not_found")
		refuse("an id that is not one", owner.get(t, "/api/v1/themes/nonsense"), http.StatusBadRequest, "bad_request")
		refuse("nobody signed in", api.anonymous().get(t, "/api/v1/themes"), http.StatusUnauthorized, "unauthorized")
		refuse("nobody signed in asking what is active", api.anonymous().get(t, "/api/v1/themes/active"), http.StatusUnauthorized, "unauthorized")
		refuse("nobody signed in importing", api.anonymous().upload(t, "/api/v1/themes/import", "x.json", []byte("{}")), http.StatusUnauthorized, "unauthorized")
	})

	t.Run("choosing, and the active answer following", func(t *testing.T) {
		none := want(t, owner.get(t, "/api/v1/themes/active"), http.StatusOK, "nothing chosen")
		if none.Body["theme"] != nil || none.Body["source"] != "" {
			t.Fatalf("a fresh account has a theme: %s", none.Raw)
		}
		chosen := want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "choose")
		if obj(t, chosen, "theme")["active"] != true {
			t.Fatalf("chosen but not active: %s", chosen.Raw)
		}
		active := want(t, owner.get(t, "/api/v1/themes/active"), http.StatusOK, "read the active theme")
		if obj(t, active, "theme")["id"] != themeID || obj(t, active, "theme")["inUse"].(float64) != 1 || active.Body["source"] != "chosen" {
			t.Fatalf("the active theme is not the chosen one: %s", active.Raw)
		}
		back := want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil}), http.StatusOK, "back to the built-in")
		if back.Body["theme"] != nil {
			t.Fatalf("null did not return to the built-in theme: %s", back.Raw)
		}
		want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "choose again")
	})

	t.Run("a private theme is nobody else's until shared, and then not theirs to change", func(t *testing.T) {
		if got := other.get(t, "/api/v1/themes/"+themeID); got.Status != http.StatusNotFound {
			t.Fatalf("a private theme was found by somebody else: %d", got.Status)
		}
		if got := other.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Mine now"}); got.Status != http.StatusNotFound {
			t.Fatalf("somebody else changed a private theme: %d", got.Status)
		}
		if seen := list(t, want(t, other.get(t, "/api/v1/themes"), http.StatusOK, "list as another"), "themes"); len(seen) != 0 {
			t.Fatalf("somebody else sees %d themes before any is shared", len(seen))
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share")
		seen := list(t, want(t, other.get(t, "/api/v1/themes"), http.StatusOK, "list as another"), "themes")
		if len(seen) != 1 || seen[0].(map[string]any)["ownerId"] != home.user.String() || seen[0].(map[string]any)["ownerName"] == "" {
			t.Fatalf("the shared theme is not seen with its owner: %v", seen)
		}
		if got := other.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Mine now"}); got.Status != http.StatusForbidden || errorCode(t, got) != "forbidden" {
			t.Fatalf("a member changed a shared theme: %d %s", got.Status, got.Raw)
		}
		if got := other.delete(t, "/api/v1/themes/"+themeID); got.Status != http.StatusForbidden {
			t.Fatalf("a member deleted a shared theme: %d %s", got.Status, got.Raw)
		}
		renamed := want(t, admin.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Magenta Tidied"}), http.StatusOK, "an administrator tidies a shared theme")
		if obj(t, renamed, "theme")["name"] != "Magenta Tidied" {
			t.Fatalf("the administrator's rename did not stick: %s", renamed.Raw)
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Magenta"}), http.StatusOK, "the owner renames it back")
		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "they use it")
		if obj(t, want(t, owner.get(t, "/api/v1/themes/"+themeID), http.StatusOK, "read"), "theme")["inUse"].(float64) != 2 {
			t.Error("two people use it now")
		}
	})

	var assetID string
	safe := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 8h12"/></svg>`)
	t.Run("files: a safe SVG is kept in the bucket and served as a download, an unsafe one is refused", func(t *testing.T) {
		added := want(t, owner.upload(t, "/api/v1/themes/"+themeID+"/assets", "glyph.svg", safe), http.StatusCreated, "upload an svg")
		asset := obj(t, added, "asset")
		if asset["contentType"] != "image/svg+xml" || asset["size"].(float64) != float64(len(safe)) {
			t.Fatalf("asset = %v", asset)
		}
		assetID = asset["id"].(string)

		stored, err := api.store.Get(context.Background(), themeObject(home.org, themeID, assetID))
		if err != nil {
			t.Fatalf("the bytes are not in the bucket: %v", err)
		}
		inBucket, _ := io.ReadAll(stored)
		stored.Close()
		if string(inBucket) != string(safe) {
			t.Fatalf("the bucket holds %q", inBucket)
		}

		got, data := owner.download(t, "/api/v1/themes/"+themeID+"/assets/"+assetID)
		if got.StatusCode != http.StatusOK || string(data) != string(safe) {
			t.Fatalf("the file came back as %d with %d bytes", got.StatusCode, len(data))
		}
		if got.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(got.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(got.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("headers = %v", got.Header)
		}
		if !strings.Contains(got.Header.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("an svg served without a policy that keeps it inert: %v", got.Header)
		}
		if got, _ := other.download(t, "/api/v1/themes/"+themeID+"/assets/"+assetID); got.StatusCode != http.StatusOK {
			t.Errorf("a member cannot read a shared theme's file: %d", got.StatusCode)
		}
		if got, _ := stranger.download(t, "/api/v1/themes/"+themeID+"/assets/"+assetID); got.StatusCode != http.StatusNotFound {
			t.Errorf("another organization read the file: %d", got.StatusCode)
		}

		if got := owner.upload(t, "/api/v1/themes/"+themeID+"/assets", "evil.svg", []byte(`<svg onload="alert(1)"><script>1</script></svg>`)); got.Status != http.StatusBadRequest {
			t.Errorf("a scripted svg: %d %s", got.Status, got.Raw)
		}
		if got := owner.upload(t, "/api/v1/themes/"+themeID+"/assets", "notes.txt", []byte("words")); got.Status != http.StatusBadRequest {
			t.Errorf("text: %d %s", got.Status, got.Raw)
		}
		if got := other.upload(t, "/api/v1/themes/"+themeID+"/assets", "glyph.svg", safe); got.Status != http.StatusForbidden {
			t.Errorf("a member added a file to somebody's theme: %d", got.Status)
		}

		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"icons": map[string]any{"home": map[string]any{"assetId": assetID}}}}), http.StatusOK, "use the file as an icon")
		if got := owner.delete(t, "/api/v1/themes/"+themeID+"/assets/"+assetID); got.Status != http.StatusConflict {
			t.Errorf("a file in use was removed: %d %s", got.Status, got.Raw)
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"spec": spec}), http.StatusOK, "stop using the file")
		want(t, owner.delete(t, "/api/v1/themes/"+themeID+"/assets/"+assetID), http.StatusNoContent, "remove the file")
		if got, _ := owner.download(t, "/api/v1/themes/"+themeID+"/assets/"+assetID); got.StatusCode != http.StatusNotFound {
			t.Errorf("a removed file still serves: %d", got.StatusCode)
		}
		if _, err := api.store.Get(context.Background(), themeObject(home.org, themeID, assetID)); !errors.Is(err, objectstore.ErrNoObject) {
			t.Errorf("a removed file is still in the bucket: %v", err)
		}
		// One more, left on the theme, so deleting the theme has something to take.
		assetID = obj(t, want(t, owner.upload(t, "/api/v1/themes/"+themeID+"/assets", "glyph.svg", safe), http.StatusCreated, "upload again"), "asset")["id"].(string)
	})

	t.Run("the organization's default is shared and is what everybody sees until they choose", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": false}), http.StatusOK, "take it private")
		if got := admin.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}); got.Status != http.StatusNotFound && got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a private theme became the default: %d %s", got.Status, got.Raw)
		}
		if got := owner.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a private theme became the default: %d %s", got.Status, got.Raw)
		}
		if _, err := h.super.Exec(context.Background(), `UPDATE org SET default_theme_id = $1 WHERE id = $2`, themeID, home.org); err == nil {
			t.Fatal("SQL let a private theme be the default")
		}
		if _, err := h.super.Exec(context.Background(), `UPDATE org SET default_theme_id = $1 WHERE id = $2`, themeID, away.org); err == nil {
			t.Fatal("SQL let another organization's theme be the default")
		}

		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share")
		if got := other.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}); got.Status != http.StatusForbidden {
			t.Fatalf("a member set the default: %d %s", got.Status, got.Raw)
		}
		set := want(t, admin.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}), http.StatusOK, "make it the default")
		if obj(t, set, "theme")["default"] != true {
			t.Fatalf("not marked as the default: %s", set.Raw)
		}

		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil}), http.StatusOK, "nothing chosen")
		for _, who := range []*client{other, admin} {
			seen := want(t, who.get(t, "/api/v1/themes/active"), http.StatusOK, "what a member sees")
			if obj(t, seen, "theme")["id"] != themeID || seen.Body["source"] != "organization" {
				t.Fatalf("a member does not see the organization's default: %s", seen.Raw)
			}
		}
		if got := want(t, stranger.get(t, "/api/v1/themes/active"), http.StatusOK, "another organization"); got.Body["theme"] != nil {
			t.Fatalf("another organization sees this one's default: %s", got.Raw)
		}
		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil, "builtIn": true}), http.StatusOK, "keep the built-in")
		if kept := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "built-in kept"); kept.Body["theme"] != nil || kept.Body["source"] != "" {
			t.Fatalf("the built-in theme was not kept: %s", kept.Raw)
		}
		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil}), http.StatusOK, "back to the default")
		if back := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "default again"); back.Body["source"] != "organization" {
			t.Fatalf("the default did not come back: %s", back.Raw)
		}

		// Taken private, the theme stops being the default at once, by SQL.
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": false}), http.StatusOK, "unshare")
		if gone := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "after unsharing"); gone.Body["theme"] != nil {
			t.Fatalf("an unshared theme is still shown as the default: %s", gone.Raw)
		}
		var still *string
		if err := h.super.QueryRow(context.Background(), `SELECT default_theme_id::text FROM org WHERE id = $1`, home.org).Scan(&still); err != nil || still != nil {
			t.Fatalf("default_theme_id after unsharing = %v, %v", still, err)
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share again")
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		if got := stranger.get(t, "/api/v1/themes/"+themeID); got.Status != http.StatusNotFound {
			t.Errorf("another organization found the theme: %d", got.Status)
		}
		if got := stranger.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}); got.Status != http.StatusNotFound {
			t.Errorf("another organization chose the theme: %d", got.Status)
		}
		if got := stranger.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Taken"}); got.Status != http.StatusNotFound {
			t.Errorf("another organization changed the theme: %d", got.Status)
		}
		if seen := list(t, want(t, stranger.get(t, "/api/v1/themes"), http.StatusOK, "list"), "themes"); len(seen) != 0 {
			t.Errorf("another organization lists %d themes", len(seen))
		}
	})

	t.Run("deleting returns everybody to the built-in theme and empties the bucket", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/themes/"+themeID), http.StatusNoContent, "delete")
		if got := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "the other's active theme"); got.Body["theme"] != nil {
			t.Errorf("a deleted theme is still somebody's: %s", got.Raw)
		}
		if got := owner.get(t, "/api/v1/themes/"+themeID); got.Status != http.StatusNotFound {
			t.Errorf("a deleted theme is still found: %d", got.Status)
		}
		if _, err := api.store.Get(context.Background(), themeObject(home.org, themeID, assetID)); !errors.Is(err, objectstore.ErrNoObject) {
			t.Errorf("a deleted theme's file is still in the bucket: %v", err)
		}
	})
}

// The service refusing is not proof: straight through SQL as stator_app, a
// tenant cannot read, write or reassign another tenant's themes.
func TestThemeRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "theme-wall-a")
	b := h.makeMember(t, "theme-wall-b")
	owner := api.as(t, a.user, a.org, h.slugOf(t, a.org))

	made := want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Walled", "shared": true}), http.StatusCreated, "make a theme")
	themeID := obj(t, made, "theme")["id"].(string)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`)
	want(t, owner.upload(t, "/api/v1/themes/"+themeID+"/assets", "r.svg", svg), http.StatusCreated, "upload")
	want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "choose")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	tables := []string{"theme", "theme_asset", "user_theme"}

	for _, table := range tables {
		if got := count(`SELECT count(*) FROM ` + table); got != 0 {
			t.Errorf("an unscoped connection sees %d rows in %s", got, table)
		}
	}
	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, b.org.String()); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if got := count(`SELECT count(*) FROM `+table+` WHERE org_id = $1`, a.org); got != 0 {
			t.Errorf("organization B sees %d of A's rows in %s", got, table)
		}
	}
	tag, err := conn.Exec(ctx, `UPDATE theme SET name = 'Taken', org_id = $2 WHERE id = $1`, themeID, b.org)
	if err != nil || tag.RowsAffected() != 0 {
		t.Errorf("organization B reached A's theme: %v rows, %v", tag.RowsAffected(), err)
	}
	tag, err = conn.Exec(ctx, `DELETE FROM theme_asset WHERE theme_id = $1`, themeID)
	if err != nil || tag.RowsAffected() != 0 {
		t.Errorf("organization B deleted A's files: %v rows, %v", tag.RowsAffected(), err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO theme (org_id, owner_id, name) VALUES ($1, $2, 'Planted')`, a.org, b.user); !isPolicyViolation(err) {
		t.Errorf("organization B planted a theme in A: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO user_theme (org_id, user_id, theme_id) VALUES ($1, $2, $3)`, b.org, b.user, themeID); err == nil {
		t.Error("organization B chose A's theme through SQL")
	}

	var name string
	err = h.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT name FROM theme WHERE id = $1`, themeID).Scan(&name)
	})
	if err != nil || name != "Walled" {
		t.Errorf("A's theme is %q (%v), want it untouched", name, err)
	}
}
