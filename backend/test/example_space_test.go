//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/example"
	"github.com/praetorianer777/stator/backend/internal/page"
)

// The example space (#288): made once per organization by an administrator,
// through the services every page goes through, and refused to everybody
// else by the service and by the database.

func nodeKinds(n document.Node, into map[string]bool) {
	into[n.Type] = true
	for _, m := range n.Marks {
		into[m.Type] = true
	}
	for _, c := range n.Content {
		nodeKinds(c, into)
	}
}

func TestAnAdministratorMakesTheExampleSpaceOnce(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-space")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	member := api.as(t, memberID, home.org, slug)
	ctx := context.Background()

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t), "orgSlug": slug}), http.StatusOK, "connect Armature")
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	h.settle(t)

	if got := want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look for the example"); got.Body["space"] != nil {
		t.Fatalf("a new organization has an example space: %s", got.Raw)
	}

	made := want(t, owner.post(t, "/api/v1/example-space", map[string]any{"language": "de"}), http.StatusCreated, "make the example in German")
	sp := obj(t, made, "space")
	spaceID := sp["id"].(string)
	if made.Body["created"] != true || sp["key"] != example.Key || sp["name"] != "Stator kennenlernen" {
		t.Fatalf("the example is %s", made.Raw)
	}

	t.Run("every page is made and published, in German, with its labels, files and calendar", func(t *testing.T) {
		var pages, posts, unpublished int
		if err := h.super.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE kind = 'page'), count(*) FILTER (WHERE kind = 'post' AND posted_at IS NOT NULL), count(*) FILTER (WHERE version = 0)
			FROM page WHERE space_id = $1`, spaceID).Scan(&pages, &posts, &unpublished); err != nil {
			t.Fatal(err)
		}
		wantPages := 1
		example.Walk(func(e example.Entry) {
			if e.Kind != page.KindFolder {
				wantPages++
			}
		})
		if pages != wantPages || posts != len(example.Posts) || unpublished != 0 {
			t.Errorf("the space holds %d pages, %d posts and %d unpublished, want %d, %d and none", pages, posts, unpublished, wantPages, len(example.Posts))
		}
		title, err := example.Title(example.German, example.Showcase)
		if err != nil {
			t.Fatal(err)
		}
		var body []byte
		if err := h.super.QueryRow(ctx, `SELECT body FROM page WHERE space_id = $1 AND title = $2`, spaceID, title).Scan(&body); err != nil {
			t.Fatalf("no showcase titled %q: %v", title, err)
		}
		root, err := document.Parse(body)
		if err != nil {
			t.Fatal(err)
		}
		shown := map[string]bool{}
		nodeKinds(root, shown)
		for name := range document.Allowed.Nodes {
			if !shown[name] {
				t.Errorf("the showcase as made shows no %q", name)
			}
		}
		for what, sql := range map[string]string{
			"files":           `SELECT count(*) FROM attachment a JOIN page p ON p.id = a.page_id WHERE p.space_id = $1`,
			"calendar events": `SELECT count(*) FROM calendar_event WHERE space_id = $1`,
			"guide labels":    `SELECT count(*) FROM page_label l JOIN page p ON p.id = l.page_id WHERE p.space_id = $1 AND l.name = 'anleitung'`,
			"comments":        `SELECT count(*) FROM comment_thread t JOIN page p ON p.id = t.page_id WHERE p.space_id = $1`,
			"tasks":           `SELECT count(*) FROM page_task k JOIN page p ON p.id = k.page_id WHERE p.space_id = $1 AND k.assignee_id = $2`,
		} {
			args := []any{spaceID}
			if what == "tasks" {
				args = append(args, home.user)
			}
			if n := h.countRows(t, sql, args...); n == 0 {
				t.Errorf("the example has no %s", what)
			}
		}
	})

	t.Run("it is audited once, as the example and not as a space", func(t *testing.T) {
		if data := h.recordedOnce(t, home.org, audit.ActionExampleSpaceCreated, &home.user, spaceID); !strings.Contains(data, `"language": "de"`) {
			t.Errorf("the record reads %s", data)
		}
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2`, home.org, audit.ActionSpaceCreated); n != 0 {
			t.Errorf("the example is recorded as %d plain spaces too", n)
		}
	})

	t.Run("a second click finds it", func(t *testing.T) {
		again := want(t, owner.post(t, "/api/v1/example-space", map[string]any{}), http.StatusOK, "make it again")
		if again.Body["created"] != false || obj(t, again, "space")["id"] != spaceID {
			t.Errorf("the second click answers %s", again.Raw)
		}
		if got := obj(t, want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look again"), "space"); got["id"] != spaceID {
			t.Errorf("the example is %v", got)
		}
		if n := h.countRows(t, `SELECT count(*) FROM space WHERE org_id = $1`, home.org); n != 1 {
			t.Errorf("the organization holds %d spaces", n)
		}
	})

	t.Run("members read it and comment, and neither ask for it nor make it", func(t *testing.T) {
		can := obj(t, want(t, member.get(t, "/api/v1/spaces/"+example.Key), http.StatusOK, "a member reads the example"), "space", "can")
		if can["addComments"] != true || can["editPages"] != false || can["administer"] != false {
			t.Errorf("a member is offered %v", can)
		}
		want(t, owner.put(t, "/api/v1/org/permissions/createSpace", map[string]any{"subjects": []any{user(memberID)}}), http.StatusOK, "let the member create spaces")
		for what, got := range map[string]response{
			"looks for it": member.get(t, "/api/v1/example-space"),
			"makes it":     member.post(t, "/api/v1/example-space", map[string]any{}),
		} {
			if got.Status != http.StatusForbidden {
				t.Errorf("a member who may create spaces %s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("an unknown language is refused by name", func(t *testing.T) {
		fieldError(t, want(t, owner.post(t, "/api/v1/example-space", map[string]any{"language": "fr"}), http.StatusUnprocessableEntity, "ask for French"), "language")
	})

	t.Run("deleted, it is made again", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/spaces/"+example.Key), http.StatusNoContent, "delete the example")
		again := want(t, owner.post(t, "/api/v1/example-space", map[string]any{}), http.StatusCreated, "make it anew")
		if sp := obj(t, again, "space"); sp["name"] != "Getting to know Stator" || sp["id"] == spaceID {
			t.Errorf("the new example is %s", again.Raw)
		}
	})
}

// A space holding the example's key leaves it the next free one.
func TestTheExampleTakesTheNextKeyWhenItsOwnIsTaken(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-key")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": example.Key, "name": "Our rotor and stator"}), http.StatusCreated, "take the key")
	made := want(t, owner.post(t, "/api/v1/example-space", map[string]any{"language": "en"}), http.StatusCreated, "make the example")
	if key := obj(t, made, "space")["key"]; key != example.Key+"2" {
		t.Errorf("the example's key is %v", key)
	}
	var body []byte
	if err := h.super.QueryRow(context.Background(), `
		SELECT p.body FROM page p JOIN space s ON s.home_page_id = p.id WHERE s.org_id = $1 AND s.example`, home.org).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"space": "`+example.Key+`2"`) {
		t.Errorf("the home page's lists do not read the example's own key: %s", body)
	}
}

// Straight through SQL as stator_app: only an administrator of the
// organization marks a space the example, one per organization, and nobody
// marks or unmarks a space after it is made.
func TestTheDatabaseKeepsTheExampleTheAdministrators(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-sql")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	want(t, owner.put(t, "/api/v1/org/permissions/createSpace", map[string]any{"subjects": []any{user(memberID)}}), http.StatusOK, "let the member create spaces")
	ordinary := obj(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "PLAIN", "name": "Plain"}), http.StatusCreated, "make a space"), "space")["id"]

	insert := `INSERT INTO space (id, org_id, key, name, created_by, example) VALUES ($1, $2, $3, 'Mine', $4, true)`
	conn := appConn(t)
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member who may create spaces making the example", insert, uuid.New(), home.org, "MINE", memberID)
	if _, err := conn.Exec(context.Background(), `INSERT INTO space (id, org_id, key, name, created_by) VALUES ($1, $2, 'MINE', 'Mine', $3)`, uuid.New(), home.org, memberID); err != nil {
		t.Fatalf("the same member cannot make an ordinary space: %v", err)
	}

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "an administrator marking a space the example", `UPDATE space SET example = true WHERE id = $1`, ordinary)
	if _, err := conn.Exec(context.Background(), insert, uuid.New(), home.org, "FIRST", home.user); err != nil {
		t.Fatalf("an administrator cannot make the example through SQL: %v", err)
	}
	refused(t, conn, "a second example", insert, uuid.New(), home.org, "SECOND", home.user)
	refused(t, conn, "unmarking the example", `UPDATE space SET example = false WHERE org_id = $1 AND example`, home.org)

	var found map[string]any
	got := want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look for the example")
	if err := json.Unmarshal(got.Raw, &found); err != nil {
		t.Fatal(err)
	}
	if sp, _ := found["space"].(map[string]any); sp == nil || sp["key"] != "FIRST" {
		t.Errorf("the example found is %s", got.Raw)
	}
}
