//go:build integration

package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/objectstore"
)

func themeObject(themeID, assetID string) string {
	return "theme/" + themeID + "/" + assetID
}

// A theme is its maker's, shared when they say so, and chosen per person.
func TestThemesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	home := h.makeMember(t, "themes")
	owner := api.as(t, home.user)
	other := api.as(t, h.addPerson(t, "member"))
	admin := api.as(t, h.addPerson(t, "admin"))

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

		stored, err := api.store.Get(context.Background(), themeObject(themeID, assetID))
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
		if _, err := api.store.Get(context.Background(), themeObject(themeID, assetID)); !errors.Is(err, objectstore.ErrNoObject) {
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
		if _, err := h.super.Exec(context.Background(), `UPDATE site SET default_theme_id = $1`, themeID); err == nil {
			t.Fatal("SQL let a private theme be the default")
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
		want(t, other.put(t, "/api/v1/themes/active", map[string]any{"themeId": nil, "builtIn": true}), http.StatusOK, "keep the built-in")
		if kept := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "built-in kept"); kept.Body["theme"] != nil || kept.Body["source"] != "" {
			t.Fatalf("the built-in theme was not kept: %s", kept.Raw)
		}
		// A print shows the organization's own, whatever its reader chose.
		if org := want(t, other.get(t, "/api/v1/themes/default"), http.StatusOK, "the organization's theme"); obj(t, org, "theme")["id"] != themeID {
			t.Fatalf("the organization's theme reads %s for a member who keeps the built-in", org.Raw)
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
		if err := h.super.QueryRow(context.Background(), `SELECT default_theme_id::text FROM site`).Scan(&still); err != nil || still != nil {
			t.Fatalf("default_theme_id after unsharing = %v, %v", still, err)
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share again")
	})

	t.Run("a member sees nothing of a private theme", func(t *testing.T) {
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": false}), http.StatusOK, "take it private")
		if got := other.get(t, "/api/v1/themes/"+themeID); got.Status != http.StatusNotFound {
			t.Errorf("a member found another's private theme: %d", got.Status)
		}
		if got, _ := other.download(t, "/api/v1/themes/"+themeID+"/assets/"+assetID); got.StatusCode != http.StatusNotFound {
			t.Errorf("a member read a private theme's file: %d", got.StatusCode)
		}
		if got := other.put(t, "/api/v1/themes/active", map[string]any{"themeId": themeID}); got.Status != http.StatusNotFound {
			t.Errorf("a member chose another's private theme: %d", got.Status)
		}
		if got := other.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Taken"}); got.Status != http.StatusNotFound {
			t.Errorf("a member changed another's private theme: %d", got.Status)
		}
		if got := admin.patch(t, "/api/v1/themes/"+themeID, map[string]any{"name": "Taken"}); got.Status != http.StatusNotFound {
			t.Errorf("an administrator changed another's private theme: %d", got.Status)
		}
		for _, seen := range list(t, want(t, other.get(t, "/api/v1/themes"), http.StatusOK, "list"), "themes") {
			if seen.(map[string]any)["id"] == themeID {
				t.Error("a member lists another's private theme")
			}
		}
		want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share again")
	})

	t.Run("deleting returns everybody to the built-in theme and empties the bucket", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/themes/"+themeID), http.StatusNoContent, "delete")
		if got := want(t, other.get(t, "/api/v1/themes/active"), http.StatusOK, "the other's active theme"); got.Body["theme"] != nil {
			t.Errorf("a deleted theme is still somebody's: %s", got.Raw)
		}
		if got := owner.get(t, "/api/v1/themes/"+themeID); got.Status != http.StatusNotFound {
			t.Errorf("a deleted theme is still found: %d", got.Status)
		}
		if _, err := api.store.Get(context.Background(), themeObject(themeID, assetID)); !errors.Is(err, objectstore.ErrNoObject) {
			t.Errorf("a deleted theme's file is still in the bucket: %v", err)
		}
	})
}

// The service refusing is not proof: straight through SQL as stator_app, a
// member cannot see another's private theme, change anybody's theme or files,
// or choose for somebody else; an administrator tidies only what is shared.
func TestThemeRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "theme-wall-a")
	b := h.makeOutsider(t, "theme-wall-b")
	admin := h.addPerson(t, "admin")
	owner := api.as(t, a.user)
	outsider := api.as(t, b.user)

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`)
	private := obj(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Walled"}), http.StatusCreated, "make a private theme"), "theme")["id"].(string)
	shared := obj(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Open", "shared": true}), http.StatusCreated, "make a shared theme"), "theme")["id"].(string)
	theirs := obj(t, want(t, outsider.post(t, "/api/v1/themes", map[string]any{"name": "Theirs"}), http.StatusCreated, "the outsider's own theme"), "theme")["id"].(string)
	for _, id := range []string{private, shared} {
		want(t, owner.upload(t, "/api/v1/themes/"+id+"/assets", "r.svg", svg), http.StatusCreated, "upload")
	}
	want(t, owner.put(t, "/api/v1/themes/active", map[string]any{"themeId": private}), http.StatusOK, "choose")

	conn := appConn(t)
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	if got := count(`SELECT count(*) FROM theme WHERE id = $1`, private); got != 0 {
		t.Error("a connection naming nobody sees a private theme")
	}
	if got := count(`SELECT count(*) FROM theme_asset WHERE theme_id = $1`, private); got != 0 {
		t.Error("a connection naming nobody sees the files of a private theme")
	}

	actAs(t, conn, b.user)
	if got := count(`SELECT count(*) FROM theme WHERE id = $1`, private); got != 0 {
		t.Error("a member sees another's private theme")
	}
	if got := count(`SELECT count(*) FROM theme_asset WHERE theme_id = $1`, private); got != 0 {
		t.Error("a member sees the files of another's private theme")
	}
	if got := count(`SELECT count(*) FROM theme WHERE id = $1`, shared); got != 1 {
		t.Error("a member does not see a shared theme")
	}
	if got := count(`SELECT count(*) FROM theme_asset WHERE theme_id = $1`, shared); got != 1 {
		t.Error("a member does not see a shared theme's file")
	}
	for _, id := range []string{private, shared} {
		untouched(t, conn, "renaming another's theme", `UPDATE theme SET name = 'Taken' WHERE id = $1`, id)
		untouched(t, conn, "taking another's theme", `UPDATE theme SET owner_id = $2 WHERE id = $1`, id, b.user)
		untouched(t, conn, "deleting another's theme", `DELETE FROM theme WHERE id = $1`, id)
		untouched(t, conn, "renaming another's file", `UPDATE theme_asset SET name = 'taken.svg' WHERE theme_id = $1`, id)
		untouched(t, conn, "deleting another's file", `DELETE FROM theme_asset WHERE theme_id = $1`, id)
		refused(t, conn, "a file on another's theme", `INSERT INTO theme_asset (theme_id, name, content_type, size) VALUES ($1, 'x.svg', 'image/svg+xml', 1)`, id)
	}
	refused(t, conn, "a theme in another's name", `INSERT INTO theme (owner_id, name) VALUES ($1, 'Planted')`, a.user)
	refused(t, conn, "handing one's theme to another", `UPDATE theme SET owner_id = $2 WHERE id = $1`, theirs, a.user)
	untouched(t, conn, "changing another's choice", `UPDATE user_theme SET theme_id = $2 WHERE user_id = $1`, a.user, shared)
	untouched(t, conn, "dropping another's choice", `DELETE FROM user_theme WHERE user_id = $1`, a.user)
	refused(t, conn, "choosing for the administrator", `INSERT INTO user_theme (user_id, theme_id) VALUES ($1, $2)`, admin, shared)
	refused(t, conn, "choosing another's private theme", `INSERT INTO user_theme (user_id, theme_id) VALUES ($1, $2)`, b.user, private)
	if _, err := conn.Exec(context.Background(), `INSERT INTO user_theme (user_id, theme_id) VALUES ($1, $2)`, b.user, shared); err != nil {
		t.Errorf("a member cannot choose a shared theme: %v", err)
	}

	actAs(t, conn, admin)
	untouched(t, conn, "an administrator renaming a private theme", `UPDATE theme SET name = 'Taken' WHERE id = $1`, private)
	untouched(t, conn, "an administrator deleting a private theme's file", `DELETE FROM theme_asset WHERE theme_id = $1`, private)
	if tag, err := conn.Exec(context.Background(), `UPDATE theme SET name = 'Tidied' WHERE id = $1`, shared); err != nil || tag.RowsAffected() != 1 {
		t.Errorf("an administrator cannot tidy a shared theme: %d rows, %v", tag.RowsAffected(), err)
	}

	var name, chosen string
	var files int
	err := h.super.QueryRow(context.Background(), `
		SELECT t.name, (SELECT count(*) FROM theme_asset f WHERE f.theme_id = t.id), u.theme_id::text
		FROM theme t JOIN user_theme u ON u.user_id = t.owner_id WHERE t.id = $1`, private).Scan(&name, &files, &chosen)
	if err != nil || name != "Walled" || files != 1 || chosen != private {
		t.Errorf("A's theme is %q with %d files, chosen %s (%v), want it untouched", name, files, chosen, err)
	}
}
