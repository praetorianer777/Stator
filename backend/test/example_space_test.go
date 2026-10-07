//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/calendar"
	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/example"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// The example space (#288): made once per organization by an administrator,
// through the services every page goes through, by the worker the request
// queues it for (#306), and refused to everybody else by the service and by
// the database.

// exampleWait bounds how long a test waits for the example to be made, by
// its own watch or by the stack's worker, whichever takes the job first.
const exampleWait = 2 * time.Minute

func nodeKinds(n document.Node, into map[string]bool) {
	into[n.Type] = true
	for _, m := range n.Marks {
		into[m.Type] = true
	}
	for _, c := range n.Content {
		nodeKinds(c, into)
	}
}

// exampleMaker is the Maker the worker runs, over the suite's services.
func (a *apiServer) exampleMaker(t *testing.T) *example.Maker {
	t.Helper()
	pages := page.NewService(a.h.cluster)
	return &example.Maker{
		Spaces: space.NewService(a.h.cluster), Pages: pages, Labels: label.NewService(a.h.cluster, pages),
		Calendars: calendar.NewService(a.h.cluster), Comments: comment.NewService(a.h.cluster), Reactions: reaction.NewService(a.h.cluster),
		Attachments: a.attachments, Armature: a.h.armature(t),
	}
}

// exampleJob runs the worker's watch until the caller's latest job is no
// longer open, and answers what GET /example-space then says.
func (a *apiServer) exampleJob(t *testing.T, c *client) response {
	t.Helper()
	watch := example.NewWatch(a.h.cluster, a.exampleMaker(t), discard(), time.Hour)
	deadline := time.Now().Add(exampleWait)
	for {
		if _, err := watch.Once(context.Background()); err != nil {
			t.Logf("the suite's watch: %v", err)
		}
		got := want(t, c.get(t, "/api/v1/example-space"), http.StatusOK, "follow the example")
		if job, _ := got.Body["job"].(map[string]any); job != nil && job["state"] != "queued" && job["state"] != "running" {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("the example was not made within %s: %s", exampleWait, got.Raw)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// makeExample asks for the example and waits until it is made.
func (a *apiServer) makeExample(t *testing.T, c *client, body map[string]any) map[string]any {
	t.Helper()
	queued := want(t, c.post(t, "/api/v1/example-space", body), http.StatusAccepted, "ask for the example")
	if job := obj(t, queued, "job"); job["state"] != "queued" && job["state"] != "running" {
		t.Fatalf("the example's job is %s", queued.Raw)
	}
	done := a.exampleJob(t, c)
	if job := obj(t, done, "job"); job["state"] != "done" || job["spaceKey"] == nil {
		t.Fatalf("the example was not made: %s", done.Raw)
	}
	return obj(t, done, "space")
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

	if got := want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look for the example"); got.Body["space"] != nil || got.Body["job"] != nil {
		t.Fatalf("a new organization has an example space: %s", got.Raw)
	}

	sp := api.makeExample(t, owner, map[string]any{"language": "de"})
	spaceID := sp["id"].(string)
	if sp["key"] != example.Key || sp["name"] != "Stator kennenlernen" {
		t.Fatalf("the example is %v", sp)
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
		var cover *string
		if err := h.super.QueryRow(ctx, `SELECT p.body, a.file_name FROM page p LEFT JOIN attachment a ON a.id = p.cover_attachment_id
			WHERE p.space_id = $1 AND p.title = $2`, spaceID, title).Scan(&body, &cover); err != nil {
			t.Fatalf("no showcase titled %q: %v", title, err)
		}
		if cover == nil || *cover != example.CoverFile {
			t.Errorf("the showcase's cover is %v, want %s", cover, example.CoverFile)
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
		if again.Body["job"] != nil || obj(t, again, "space")["id"] != spaceID {
			t.Errorf("the second click answers %s", again.Raw)
		}
		got := want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look again")
		if obj(t, got, "space")["id"] != spaceID || obj(t, got, "job")["spaceKey"] != example.Key {
			t.Errorf("the example is %s", got.Raw)
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
		if sp := api.makeExample(t, owner, map[string]any{}); sp["name"] != "Getting to know Stator" || sp["id"] == spaceID {
			t.Errorf("the new example is %v", sp)
		}
	})
}

// Clicks while the example is queued or being made find the one job; the
// space is made once.
func TestClicksWhileTheExampleIsMadeFindTheOneJob(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-clicks")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))

	const clicks = 5
	answers := make([]response, clicks)
	var wg sync.WaitGroup
	for i := range clicks {
		wg.Go(func() { answers[i] = owner.post(t, "/api/v1/example-space", map[string]any{"language": "en"}) })
	}
	wg.Wait()
	var id any
	for _, got := range answers {
		job := obj(t, want(t, got, http.StatusAccepted, "click"), "job")
		if id == nil {
			id = job["id"]
		}
		if job["id"] != id {
			t.Errorf("the clicks queued jobs %v and %v", id, job["id"])
		}
	}
	again := want(t, owner.post(t, "/api/v1/example-space", map[string]any{}), http.StatusAccepted, "click once more")
	if obj(t, again, "job")["id"] != id {
		t.Errorf("a later click found %s", again.Raw)
	}
	if got := obj(t, api.exampleJob(t, owner), "job"); got["state"] != "done" || got["id"] != id {
		t.Errorf("the job ended %v", got)
	}
	if n := h.countRows(t, `SELECT count(*) FROM example_job WHERE org_id = $1`, home.org); n != 1 {
		t.Errorf("%d jobs were queued", n)
	}
	if n := h.countRows(t, `SELECT count(*) FROM space WHERE org_id = $1 AND example`, home.org); n != 1 {
		t.Errorf("%d examples were made", n)
	}
}

// slowDown makes every statement of the kind on table in org sleep, until
// the test ends.
func (h *harness) slowDown(t *testing.T, org uuid.UUID, table, kind string, sleep time.Duration) {
	t.Helper()
	h.orgTrigger(t, org, table, kind, fmt.Sprintf(`PERFORM pg_sleep(%f);`, sleep.Seconds()))
}

// orgTrigger runs body before each row the kind of statement on table writes
// in org, until the test ends.
func (h *harness) orgTrigger(t *testing.T, org uuid.UUID, table, kind, body string) {
	t.Helper()
	name := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	ctx := context.Background()
	if _, err := h.super.Exec(ctx, fmt.Sprintf(`
		CREATE FUNCTION %[1]s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN %[2]s RETURN NEW; END $$;
		CREATE TRIGGER %[1]s BEFORE %[3]s ON %[4]s FOR EACH ROW WHEN (NEW.org_id = '%[5]s') EXECUTE FUNCTION %[1]s();`,
		name, body, kind, table, org)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, fmt.Sprintf(`DROP TRIGGER IF EXISTS %[1]s ON %[2]s`, name, table))
		h.cleanupExec(t, h.super, fmt.Sprintf(`DROP FUNCTION IF EXISTS %[1]s()`, name))
	})
}

// A making slower than a request may last is answered at once and made all
// the same, where it used to answer 500 when the limit ran out.
func TestTheExampleOutlastsTheRequestLimit(t *testing.T) {
	h := newHarness(t)
	const limit = 2 * time.Second
	api := newAPIServer(t, h, func(s *httpapi.Server) { s.RequestTimeout = limit })
	home := h.makeMember(t, "example-slow")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	h.slowDown(t, home.org, "space", "INSERT", limit+time.Second)

	began := time.Now()
	sp := api.makeExample(t, owner, map[string]any{"language": "en"})
	if took := time.Since(began); took < limit {
		t.Errorf("the slowed database made the example in %s, under the request limit of %s", took, limit)
	}
	if sp["key"] != example.Key {
		t.Errorf("the example is %v", sp)
	}
}

// A making that fails part way leaves no space, no record of one, and a
// sentence that says what to do.
func TestAFailedExampleLeavesNothingBehind(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-fails")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	// The calendar comes after the space and its pages are made.
	h.orgTrigger(t, home.org, "calendar", "INSERT", `RAISE EXCEPTION 'the test refuses calendars';`)

	want(t, owner.post(t, "/api/v1/example-space", map[string]any{"language": "en"}), http.StatusAccepted, "ask for the example")
	got := api.exampleJob(t, owner)
	job := obj(t, got, "job")
	if job["state"] != "failed" || job["failure"] != string(example.FailureFailed) || job["message"] != example.FailureFailed.Message() || got.Body["space"] != nil {
		t.Fatalf("the failed making reads %s", got.Raw)
	}
	for what, sql := range map[string]string{
		"spaces":    `SELECT count(*) FROM space WHERE org_id = $1`,
		"pages":     `SELECT count(*) FROM page WHERE org_id = $1`,
		"files":     `SELECT count(*) FROM attachment WHERE org_id = $1`,
		"audit log": `SELECT count(*) FROM audit_log WHERE org_id = $1 AND target_type = 'space'`,
	} {
		if n := h.countRows(t, sql, home.org); n != 0 {
			t.Errorf("the failed making left %d %s", n, what)
		}
	}
	queued := want(t, owner.post(t, "/api/v1/example-space", map[string]any{}), http.StatusAccepted, "try again")
	if obj(t, queued, "job")["id"] == job["id"] {
		t.Errorf("trying again found the failed job: %s", queued.Raw)
	}
}

// A making whose context ends part way, as a request's did at its limit,
// still deletes what it made.
func TestAMakingCutShortDeletesItsSpace(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-cut")
	ctx, cut := context.WithCancel(home.ctx)
	defer cut()
	maker := api.exampleMaker(t)
	var made uuid.UUID
	maker.Started = func(_ context.Context, sp *space.Space) error {
		made = sp.ID
		cut()
		return nil
	}
	_, _, _, err := maker.Make(ctx, perm.Actor{UserID: home.user, Role: "owner"}, example.Person{ID: home.user, Name: "Owner"}, example.English)
	if err == nil || made == uuid.Nil {
		t.Fatalf("the cut making answered %v, made %v", err, made)
	}
	if n := h.countRows(t, `SELECT count(*) FROM space WHERE id = $1`, made); n != 0 {
		t.Errorf("the cut making left its space")
	}
}

// A space holding the example's key leaves it the next free one.
func TestTheExampleTakesTheNextKeyWhenItsOwnIsTaken(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-key")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": example.Key, "name": "Our rotor and stator"}), http.StatusCreated, "take the key")
	if key := api.makeExample(t, owner, map[string]any{"language": "en"})["key"]; key != example.Key+"2" {
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

// Straight through SQL as stator_app: only an administrator queues the
// example, for themselves and as a job still to run, one at a time; what
// the job then does is the worker's alone to write, and members read none.
func TestTheDatabaseKeepsTheExampleJobsTheAdministrators(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "example-job-sql")
	memberID := h.addPerson(t, home.org, "member")
	ctx := context.Background()
	insert := `INSERT INTO example_job (id, org_id, requested_by, language) VALUES ($1, $2, $3, 'en')`

	conn := appConn(t)
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member queuing the example", insert, uuid.New(), home.org, memberID)

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "an administrator queuing it for somebody else", insert, uuid.New(), home.org, memberID)
	refused(t, conn, "an administrator queuing a job already done",
		`INSERT INTO example_job (id, org_id, requested_by, language, state) VALUES ($1, $2, $3, 'en', 'done')`, uuid.New(), home.org, home.user)
	job := uuid.New()
	if _, err := conn.Exec(ctx, insert, job, home.org, home.user); err != nil {
		t.Fatalf("an administrator cannot queue the example through SQL: %v", err)
	}
	refused(t, conn, "a second job while one waits", insert, uuid.New(), home.org, home.user)
	for what, sql := range map[string]string{
		"marking it done":  `UPDATE example_job SET state = 'done' WHERE id = $1`,
		"deleting it":      `DELETE FROM example_job WHERE id = $1`,
		"naming its space": `UPDATE example_job SET space_id = NULL WHERE id = $1`,
	} {
		refused(t, conn, "an administrator "+what, sql, job)
	}
	var seen int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM example_job WHERE id = $1`, job).Scan(&seen); err != nil || seen != 1 {
		t.Errorf("the administrator reads %d of their job: %v", seen, err)
	}
	actAs(t, conn, home.org, memberID)
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM example_job WHERE org_id = $1`, home.org).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("a member reads %d jobs: %v", seen, err)
	}
}
