//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/task"
)

// todo is one checklist item: its words, then whom it mentions and the day it
// names, either of which may be left out; id keeps it the same task.
type todo struct {
	id       string
	text     string
	assignee uuid.UUID
	due      string
	done     bool
	nested   []todo
}

func (td todo) node() map[string]any {
	inline := []any{map[string]any{"type": "text", "text": td.text}}
	if td.assignee != uuid.Nil {
		inline = append(inline, map[string]any{"type": "text", "text": " "},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": td.assignee.String(), "label": "Someone"}})
	}
	if td.due != "" {
		inline = append(inline, map[string]any{"type": "text", "text": " "}, map[string]any{"type": "date", "attrs": map[string]any{"date": td.due}})
	}
	attrs := map[string]any{"checked": td.done}
	if td.id != "" {
		attrs["taskId"] = td.id
	}
	content := []any{map[string]any{"type": "paragraph", "content": inline}}
	if len(td.nested) > 0 {
		content = append(content, todoList(td.nested...))
	}
	return map[string]any{"type": "taskItem", "attrs": attrs, "content": content}
}

func todoList(items ...todo) map[string]any {
	nodes := make([]any, len(items))
	for i, item := range items {
		nodes[i] = item.node()
	}
	return map[string]any{"type": "taskList", "content": nodes}
}

// todoDoc is a page of one paragraph, then a checklist of the items.
func todoDoc(lead string, items ...todo) map[string]any {
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": lead}}},
		todoList(items...),
	}}
}

// taskIDsOf reads the ids the published body gave its checklist items, by
// their first words.
func taskIDsOf(t *testing.T, c *client, page string) map[string]string {
	t.Helper()
	out := map[string]string{}
	body := obj(t, want(t, c.get(t, pagePath(page)), http.StatusOK, "read the page"), "page")["body"].(map[string]any)
	var walk func(nodes []any)
	walk = func(nodes []any) {
		for _, each := range nodes {
			n := each.(map[string]any)
			content, _ := n["content"].([]any)
			if n["type"] == "taskItem" {
				attrs := n["attrs"].(map[string]any)
				id, _ := attrs["taskId"].(string)
				if _, err := uuid.Parse(id); err != nil {
					t.Errorf("a published item has the id %v", attrs["taskId"])
				}
				first := content[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
				out[first] = id
			}
			walk(content)
		}
	}
	walk(body["content"].([]any))
	return out
}

// tasksOf is a person's tasks of one state, as the list hands out its first window.
func tasksOf(t *testing.T, c *client, state string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, each := range list(t, want(t, c.get(t, "/api/v1/tasks?limit=100&state="+state), http.StatusOK, "the "+state+" tasks"), "tasks") {
		out = append(out, each.(map[string]any))
	}
	return out
}

func textsOf(tasks []map[string]any) []string {
	out := make([]string, len(tasks))
	for i, each := range tasks {
		out[i] = each["text"].(string)
	}
	return out
}

func kindOf(t *testing.T, c *client, kind notify.Kind) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, n := range notificationsOf(t, c, "") {
		if n["kind"] == string(kind) {
			out = append(out, n)
		}
	}
	return out
}

// walkTasks follows the list's cursors a window of one at a time and returns
// every task's words it met.
func walkTasks(t *testing.T, c *client, state string) []string {
	t.Helper()
	var out []string
	cursor := ""
	for range 50 {
		r := want(t, c.get(t, "/api/v1/tasks?state="+state+"&limit=1&cursor="+url.QueryEscape(cursor)), http.StatusOK, "a window of tasks")
		got := list(t, r, "tasks")
		if len(got) > 1 {
			t.Fatalf("a window of one held %v", got)
		}
		for _, each := range got {
			out = append(out, each.(map[string]any)["text"].(string))
		}
		next, _ := r.Body["next"].(string)
		if next == "" {
			return out
		}
		cursor = next
	}
	t.Fatal("the walk did not end")
	return nil
}

// Tasks are read from the published page: assigning one tells the assignee
// once, the list shows each person their own on pages they may view, and
// ticking one off publishes the page.
func TestTasksOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	org := h.makeMember(t, "tasks")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, benID, carlID := h.namedPerson(t, org.org, "Ann Doer"), h.namedPerson(t, org.org, "Ben Doer"), h.namedPerson(t, org.org, "Carl Outsider")
	ann, ben, carl := api.as(t, annID, org.org, slug), api.as(t, benID, org.org, slug), api.as(t, carlID, org.org, slug)

	docs := newTree(t, owner, "TSK", "Tasks")
	minutes := docs.add(docs.homeID, "Minutes")
	secret := docs.add(docs.homeID, "Secret plans")
	want(t, restrict(t, owner, secret, []any{user(org.user), user(annID)}, nil), http.StatusOK, "only ann sees the plans")
	h.settle(t)

	var ids map[string]string
	t.Run("publishing makes tasks of the checklist and tells each assignee once", func(t *testing.T) {
		publishBody(t, owner, minutes, todoDoc("Weekly sync.",
			todo{text: "Send the notes", assignee: annID, due: "2026-11-02"},
			todo{text: "Book a room", assignee: benID, nested: []todo{{text: "Ask facilities", assignee: annID}}},
			todo{text: "Nobody's yet"},
		))
		h.drained(t, org.org)
		ids = taskIDsOf(t, owner, minutes)
		if len(ids) != 4 {
			t.Fatalf("the published items have the ids %v", ids)
		}
		got := tasksOf(t, ann, "open")
		sameList(t, "ann's open tasks", textsOf(got), "Send the notes @Someone 2026-11-02", "Ask facilities @Someone")
		first := got[0]
		page := first["page"].(map[string]any)
		if first["id"] != ids["Send the notes"] || first["dueOn"] != "2026-11-02" || first["done"] != false || first["canEdit"] != true ||
			page["id"] != minutes || page["title"] != "Minutes" || page["spaceKey"] != "TSK" || first["assigneeId"] != annID.String() || first["assignedByName"] == "" {
			t.Errorf("ann's first task reads %v", first)
		}
		told := kindOf(t, ann, notify.KindAssigned)
		if len(told) != 1 || told[0]["page"].(map[string]any)["id"] != minutes || told[0]["excerpt"] != "Send the notes @Someone 2026-11-02" || told[0]["actorId"] != org.user.String() {
			t.Errorf("ann was told %v", told)
		}
		if told[0]["taskId"] != ids["Send the notes"] {
			t.Errorf("the notification names the task %v, not %v", told[0]["taskId"], ids["Send the notes"])
		}
		if want := "/s/TSK/p/" + minutes + "#task-" + ids["Send the notes"]; first["path"] != want {
			t.Errorf("the task's path is %v, not %s", first["path"], want)
		}
		if got := kindOf(t, ann, notify.KindMentioned); len(got) != 0 {
			t.Errorf("an assignment also told ann she was mentioned: %v", got)
		}
		if told := kindOf(t, ben, notify.KindAssigned); len(told) != 1 {
			t.Errorf("ben was told %v", told)
		}
		if got := notificationsOf(t, owner, ""); len(got) != 0 {
			t.Errorf("the author was told of their own tasks: %v", got)
		}
	})

	t.Run("a republish keeps the tasks and tells nobody again; a new assignee alone hears", func(t *testing.T) {
		publishBody(t, owner, minutes, todoDoc("Weekly sync, revised.",
			todo{id: ids["Send the notes"], text: "Send the notes", assignee: annID, due: "2026-11-03"},
			todo{id: ids["Book a room"], text: "Book a room", assignee: annID},
			todo{text: "Nobody's yet"},
		))
		h.drained(t, org.org)
		if got := taskIDsOf(t, owner, minutes); got["Send the notes"] != ids["Send the notes"] || got["Nobody's yet"] != ids["Nobody's yet"] {
			t.Errorf("the tasks changed ids: %v, were %v", got, ids)
		}
		sameList(t, "ann's open tasks", textsOf(tasksOf(t, ann, "open")), "Send the notes @Someone 2026-11-03", "Book a room @Someone")
		if told := kindOf(t, ann, notify.KindAssigned); len(told) != 2 || told[0]["excerpt"] != "Book a room @Someone" {
			t.Errorf("ann was told %v", told)
		}
		if got := tasksOf(t, ben, "open"); len(got) != 0 {
			t.Errorf("ben keeps the room he was taken off: %v", got)
		}
	})

	t.Run("a task is assigned only to a member who may view the page", func(t *testing.T) {
		p := obj(t, want(t, owner.get(t, pagePath(secret)), http.StatusOK, "read the plans"), "page")
		want(t, owner.put(t, pagePath(secret, "/draft"), map[string]any{"title": "Secret plans", "body": todoDoc("Plans.", todo{text: "Carl does it", assignee: carlID}), "baseVersion": p["version"]}), http.StatusOK, "a draft naming carl")
		r := want(t, owner.post(t, pagePath(secret, "/publish"), map[string]any{}), http.StatusUnprocessableEntity, "carl may not view the plans")
		if fields, _ := r.Body["error"].(map[string]any)["fields"].(map[string]any); fields["body"] == nil {
			t.Errorf("the refusal does not say what to change: %v", r.Body)
		}
		publishBody(t, owner, secret, todoDoc("Plans.", todo{text: "A stranger does it", assignee: uuid.New()}, todo{text: "Ann does it", assignee: annID}))
		h.drained(t, org.org)
		if n := h.countRows(t, `SELECT count(*) FROM page_task WHERE org_id = $1 AND page_id = $2 AND assignee_id IS NULL`, org.org, secret); n != 1 {
			t.Errorf("%d tasks of the plans are unassigned, want the stranger's", n)
		}
		if got := tasksOf(t, carl, "open"); len(got) != 0 {
			t.Errorf("carl lists %v", got)
		}
	})

	t.Run("the list leaves out what the assignee may no longer view", func(t *testing.T) {
		if !has(textsOf(tasksOf(t, ann, "open")), "Ann does it @Someone") {
			t.Fatal("ann does not list her task on the plans")
		}
		want(t, restrict(t, owner, secret, []any{user(org.user)}, nil), http.StatusOK, "the plans close to ann")
		h.settle(t)
		if has(textsOf(tasksOf(t, ann, "open")), "Ann does it @Someone") {
			t.Error("ann lists a task on a page closed to her")
		}
		want(t, ann.patch(t, pagePath(secret, "/tasks/"+uuid.NewString()), map[string]any{"done": true}), http.StatusNotFound, "ann ticks a task she may not see")
		want(t, restrict(t, owner, secret, []any{user(org.user), user(annID)}, nil), http.StatusOK, "the plans open to ann again")
		h.settle(t)
		if !has(textsOf(tasksOf(t, ann, "open")), "Ann does it @Someone") {
			t.Error("ann's task did not come back with her access")
		}
	})

	t.Run("ticking a task off publishes the page once", func(t *testing.T) {
		before := number(obj(t, want(t, owner.get(t, pagePath(minutes)), http.StatusOK, "read the minutes"), "page")["version"])
		got := obj(t, want(t, ann.patch(t, pagePath(minutes, "/tasks/"+ids["Send the notes"]), map[string]any{"done": true}), http.StatusOK, "ann ticks hers off"), "task")
		if got["done"] != true || got["doneAt"] == nil || got["id"] != ids["Send the notes"] {
			t.Errorf("the task reads %v", got)
		}
		want(t, ann.patch(t, pagePath(minutes, "/tasks/"+ids["Send the notes"]), map[string]any{"done": true}), http.StatusOK, "and again")
		h.drained(t, org.org)
		p := obj(t, want(t, owner.get(t, pagePath(minutes)), http.StatusOK, "read the minutes"), "page")
		if number(p["version"]) != before+1 {
			t.Errorf("ticking twice made version %v of %d", p["version"], before)
		}
		versions := list(t, want(t, owner.get(t, pagePath(minutes, "/versions")), http.StatusOK, "the history"), "versions")
		if c := versions[0].(map[string]any)["comment"]; c != "Ticked off a task: Send the notes @Someone 2026-11-03" {
			t.Errorf("the version says %v", c)
		}
		sameList(t, "ann's open tasks", textsOf(tasksOf(t, ann, "open")), "Book a room @Someone", "Ann does it @Someone")
		sameList(t, "ann's done tasks", textsOf(tasksOf(t, ann, "done")), "Send the notes @Someone 2026-11-03")
		got = obj(t, want(t, ann.patch(t, pagePath(minutes, "/tasks/"+ids["Send the notes"]), map[string]any{"done": false}), http.StatusOK, "ann opens it again"), "task")
		if got["done"] != false || got["doneAt"] != nil {
			t.Errorf("the reopened task reads %v", got)
		}
	})

	t.Run("open tasks walk soonest first and those without a day last, done ones latest first", func(t *testing.T) {
		pile := docs.add(docs.homeID, "Pile")
		publishBody(t, owner, pile, todoDoc("Ann's pile.",
			todo{text: "Later", assignee: annID, due: "2027-01-15"},
			todo{text: "Soon", assignee: annID, due: "2026-10-20"},
			todo{text: "Whenever", assignee: annID},
			todo{text: "Also soon", assignee: annID, due: "2026-10-20"},
		))
		h.settle(t)
		got := walkTasks(t, ann, "open")
		if len(got) != 7 || got[0] != "Soon @Someone 2026-10-20" && got[0] != "Also soon @Someone 2026-10-20" ||
			got[2] != "Send the notes @Someone 2026-11-03" || got[3] != "Later @Someone 2027-01-15" {
			t.Errorf("the walk met %v", got)
		}
		seen := map[string]bool{}
		for _, text := range got {
			if seen[text] {
				t.Errorf("the walk met %q twice", text)
			}
			seen[text] = true
		}
		for _, text := range got[4:] {
			if text != "Whenever @Someone" && text != "Book a room @Someone" && text != "Ann does it @Someone" {
				t.Errorf("a task with a day came after those without: %v", got)
			}
		}
		ticked := taskIDsOf(t, owner, pile)
		want(t, ann.patch(t, pagePath(pile, "/tasks/"+ticked["Later"]), map[string]any{"done": true}), http.StatusOK, "ann ticks one")
		time.Sleep(10 * time.Millisecond)
		want(t, ann.patch(t, pagePath(pile, "/tasks/"+ticked["Whenever"]), map[string]any{"done": true}), http.StatusOK, "then another")
		h.settle(t)
		sameList(t, "the done walk", walkTasks(t, ann, "done"), "Whenever @Someone", "Later @Someone 2027-01-15")
	})

	t.Run("a copy takes the tasks and tells nobody", func(t *testing.T) {
		before := len(kindOf(t, ann, notify.KindAssigned))
		want(t, owner.post(t, pagePath(minutes, "/copy"), map[string]any{"parentId": docs.homeID, "title": "Minutes copy"}), http.StatusCreated, "copy the minutes")
		h.drained(t, org.org)
		n := 0
		for _, each := range tasksOf(t, ann, "open") {
			if each["page"].(map[string]any)["title"] == "Minutes copy" {
				n++
			}
		}
		if n != 2 {
			t.Errorf("ann lists %d tasks of the copy", n)
		}
		if after := len(kindOf(t, ann, notify.KindAssigned)); after != before {
			t.Errorf("the copy told ann %d times", after-before)
		}
	})

	t.Run("a task in the trash or the archive is not listed", func(t *testing.T) {
		pile := ""
		for _, each := range tasksOf(t, ann, "open") {
			if p := each["page"].(map[string]any); p["title"] == "Pile" {
				pile = p["id"].(string)
			}
		}
		want(t, owner.delete(t, pagePath(pile)), http.StatusNoContent, "the pile goes to the trash")
		h.settle(t)
		for _, each := range tasksOf(t, ann, "open") {
			if each["page"].(map[string]any)["id"] == pile {
				t.Errorf("ann lists %v from the trash", each)
			}
		}
		want(t, owner.put(t, pagePath(minutes, "/archive"), nil), http.StatusOK, "the minutes are archived")
		h.settle(t)
		for _, each := range tasksOf(t, ann, "open") {
			if each["page"].(map[string]any)["id"] == minutes {
				t.Errorf("ann lists %v from the archive", each)
			}
		}
		want(t, ann.patch(t, pagePath(minutes, "/tasks/"+ids["Book a room"]), map[string]any{"done": true}), http.StatusConflict, "ticking a task of an archived page")
		want(t, owner.delete(t, pagePath(minutes, "/archive")), http.StatusOK, "the minutes come back")
	})

	t.Run("refusals", func(t *testing.T) {
		readerID := h.namedPerson(t, org.org, "Rita Reader")
		reader := api.as(t, readerID, org.org, slug)
		want(t, restrict(t, owner, minutes, nil, []any{user(org.user), user(annID)}), http.StatusOK, "only ann and the owner edit the minutes")
		h.settle(t)
		want(t, reader.patch(t, pagePath(minutes, "/tasks/"+ids["Book a room"]), map[string]any{"done": true}), http.StatusForbidden, "a reader ticks a task")
		want(t, ann.patch(t, pagePath(minutes, "/tasks/"+uuid.NewString()), map[string]any{"done": true}), http.StatusNotFound, "no such task")
		want(t, ann.patch(t, pagePath(uuid.NewString(), "/tasks/"+ids["Book a room"]), map[string]any{"done": true}), http.StatusNotFound, "no such page")
		want(t, ann.patch(t, pagePath(minutes, "/tasks/not-an-id"), map[string]any{"done": true}), http.StatusBadRequest, "a task id that is no id")
		want(t, ann.get(t, "/api/v1/tasks?state=someday"), http.StatusUnprocessableEntity, "a state the list does not know")
		want(t, ann.get(t, "/api/v1/tasks?cursor=nonsense"), http.StatusUnprocessableEntity, "a cursor not given out")
		want(t, ann.get(t, "/api/v1/tasks?limit=1000"), http.StatusUnprocessableEntity, "too large a window")
		want(t, api.anonymous().get(t, "/api/v1/tasks"), http.StatusUnauthorized, "nobody signed in")
		if got := tasksOf(t, reader, "open"); len(got) != 0 {
			t.Errorf("rita lists %v", got)
		}
		if task.MaxLimit < 100 {
			t.Errorf("the list's largest window is %d", task.MaxLimit)
		}
	})
}

// The worker reminds an assignee once on the day a task is due, and never of a
// day already past when it was set, nor of a task done.
func TestADueTaskRemindsItsAssignee(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	org := h.makeMember(t, "due-tasks")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID := h.namedPerson(t, org.org, "Ann Due")
	ann := api.as(t, annID, org.org, slug)
	watch := task.NewDueWatch(h.cluster, discard(), time.Hour)
	ctx := context.Background()

	today := time.Now().UTC().Format(time.DateOnly)
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format(time.DateOnly)
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format(time.DateOnly)
	docs := newTree(t, owner, "DUE", "Due")
	list := docs.add(docs.homeID, "List")
	publishBody(t, owner, list, todoDoc("Due things.",
		todo{text: "Today", assignee: annID, due: today},
		todo{text: "Yesterday", assignee: annID, due: yesterday},
		todo{text: "Tomorrow", assignee: annID, due: tomorrow},
		todo{text: "Done today", assignee: annID, due: today, done: true},
	))
	h.drained(t, org.org)
	if _, err := watch.Once(ctx); err != nil {
		t.Fatal(err)
	}
	h.drained(t, org.org)
	due := kindOf(t, ann, notify.KindDue)
	if len(due) != 1 || due[0]["excerpt"] != "Today @Someone "+today || due[0]["actorId"] != nil {
		t.Fatalf("ann was reminded of %v", due)
	}
	if due[0]["taskId"] == nil {
		t.Errorf("the reminder does not name its task: %v", due[0])
	}
	if n, err := watch.Once(ctx); err != nil || n != 0 {
		t.Errorf("a second look noted %d, %v", n, err)
	}

	ids := taskIDsOf(t, owner, list)
	publishBody(t, owner, list, todoDoc("Due things, moved.",
		todo{id: ids["Today"], text: "Today", assignee: annID, due: today},
		todo{id: ids["Yesterday"], text: "Yesterday", assignee: annID, due: yesterday},
		todo{id: ids["Tomorrow"], text: "Tomorrow", assignee: annID, due: today},
	))
	h.drained(t, org.org)
	if _, err := watch.Once(ctx); err != nil {
		t.Fatal(err)
	}
	h.drained(t, org.org)
	due = kindOf(t, ann, notify.KindDue)
	if len(due) != 2 || due[0]["excerpt"] != "Tomorrow @Someone "+today {
		t.Errorf("after moving a day to today ann was reminded of %v", due)
	}
	if n := h.countRows(t, `SELECT count(*) FROM page_task WHERE org_id = $1 AND due_noticed_at IS NULL`, org.org); n != 0 {
		t.Errorf("%d tasks still wait for a reminder", n)
	}
}

// Straight through SQL, tasks are written only by an editor of the published
// page, assigned only to people who may view it, and stamped by the database.
func TestTasksAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	org := h.makeMember(t, "task-rls")
	slug := h.slugOf(t, org.org)
	owner := api.as(t, org.user, org.org, slug)
	annID, carlID, ritaID := h.addPerson(t, org.org, "member"), h.addPerson(t, org.org, "member"), h.addPerson(t, org.org, "member")

	docs := newTree(t, owner, "TRLS", "Raw tasks")
	page := docs.add(docs.homeID, "Page")
	secret := docs.add(docs.homeID, "Secret")
	sketch := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": docs.homeID, "title": "Sketch"}), http.StatusCreated, "an unpublished page"), "page")["id"].(string)
	want(t, restrict(t, owner, page, nil, []any{user(org.user), user(annID)}), http.StatusOK, "only ann and the owner edit the page")
	want(t, restrict(t, owner, secret, []any{user(org.user), user(annID)}, nil), http.StatusOK, "carl may not see the secret")
	publishBody(t, owner, secret, todoDoc("Secret.", todo{text: "Ann's secret task", assignee: annID}))
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	insert := `INSERT INTO page_task (org_id, page_id, task_id, position, summary, assignee_id) VALUES ($1, $2, $3, 0, 'Forged', $4)`

	t.Run("only an editor of a published page writes its tasks", func(t *testing.T) {
		actAs(t, conn, org.org, ritaID)
		denied(t, conn, "a reader adds a task", insert, org.org, page, uuid.New(), nil)
		actAs(t, conn, org.org, org.user)
		denied(t, conn, "a task on an unpublished page", insert, org.org, sketch, uuid.New(), nil)
		if _, err := conn.Exec(ctx, insert, org.org, page, uuid.New(), annID); err != nil {
			t.Errorf("the owner may not write a task of their page: %v", err)
		}
		actAs(t, conn, org.org, carlID)
		untouched(t, conn, "carl ticks the secret's tasks", `UPDATE page_task SET done = true WHERE page_id = $1`, secret)
		untouched(t, conn, "carl deletes the secret's tasks", `DELETE FROM page_task WHERE page_id = $1`, secret)
		var seen int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM page_task WHERE page_id = $1`, secret).Scan(&seen); err != nil || seen != 0 {
			t.Errorf("carl reads %d tasks of the secret, %v", seen, err)
		}
	})

	t.Run("a task is assigned only to somebody who may view the page", func(t *testing.T) {
		actAs(t, conn, org.org, org.user)
		denied(t, conn, "carl assigned on the secret", insert, org.org, secret, uuid.New(), carlID)
		denied(t, conn, "carl given the secret's task", `UPDATE page_task SET assignee_id = $2 WHERE page_id = $1`, secret, carlID)
	})

	t.Run("who assigned, in which version and the reminder are the database's", func(t *testing.T) {
		actAs(t, conn, org.org, org.user)
		denied(t, conn, "the app notes a reminder", `UPDATE page_task SET due_noticed_at = now() WHERE page_id = $1`, secret)
		denied(t, conn, "the app names who assigned", `UPDATE page_task SET assigned_by = $2 WHERE page_id = $1`, secret, annID)
		denied(t, conn, "the app backdates a task done", `UPDATE page_task SET done_at = now() - interval '1 day' WHERE page_id = $1`, secret)
		denied(t, conn, "the app forges the version", `INSERT INTO page_task (org_id, page_id, task_id, position, summary, assigned_version) VALUES ($1, $2, $3, 0, 'x', 7)`, org.org, page, uuid.New())
		if _, err := conn.Exec(ctx, `UPDATE page_task SET done = true WHERE page_id = $1`, secret); err != nil {
			t.Fatal(err)
		}
		var (
			by      uuid.UUID
			version int
			doneAt  *time.Time
		)
		if err := h.super.QueryRow(ctx, `SELECT assigned_by, assigned_version, done_at FROM page_task WHERE page_id = $1`, secret).Scan(&by, &version, &doneAt); err != nil {
			t.Fatal(err)
		}
		if by != org.user || version != 2 || doneAt == nil {
			t.Errorf("the stamp reads %v, version %d, done at %v", by, version, doneAt)
		}
	})

	t.Run("somebody who leaves leaves their tasks unassigned", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/users/"+annID.String()), http.StatusNoContent, "ann leaves")
		h.settle(t)
		if n := h.countRows(t, `SELECT count(*) FROM page_task WHERE org_id = $1 AND assignee_id IS NOT NULL`, org.org); n != 0 {
			t.Errorf("%d tasks are still assigned to somebody gone", n)
		}
	})
}

// A token limited to spaces lists and ticks only the tasks in them, and
// straight through SQL reads and writes no task elsewhere, though its owner
// reaches every one.
func TestTasksFollowASpaceLimitedToken(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "task-token")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)

	near := newTree(t, owner, "TNEAR", "Near")
	far := newTree(t, owner, "TFAR", "Far")
	nearPage, farPage := near.add(near.homeID, "Near list"), far.add(far.homeID, "Far list")
	publishBody(t, owner, nearPage, todoDoc("Near.", todo{text: "Near task", assignee: home.user}))
	publishBody(t, owner, farPage, todoDoc("Far.", todo{text: "Far task", assignee: home.user}))
	nearID := obj(t, want(t, owner.get(t, "/api/v1/spaces/TNEAR"), http.StatusOK, "TNEAR"), "space")["id"].(string)
	farTask := taskIDsOf(t, owner, farPage)["Far task"]
	nearTask := taskIDsOf(t, owner, nearPage)["Near task"]
	_, secret := makeToken(t, owner, map[string]any{"name": "near only", "spaces": []string{"TNEAR"}})
	limited := api.withToken(secret)
	h.settle(t)

	sameList(t, "the owner's tasks", textsOf(tasksOf(t, owner, "open")), "Near task @Someone", "Far task @Someone")
	sameList(t, "the token's tasks", textsOf(tasksOf(t, limited, "open")), "Near task @Someone")
	want(t, limited.patch(t, pagePath(farPage, "/tasks/"+farTask), map[string]any{"done": true}), http.StatusNotFound, "the token ticks a task elsewhere")
	want(t, limited.patch(t, pagePath(nearPage, "/tasks/"+nearTask), map[string]any{"done": true}), http.StatusOK, "the token ticks a task in its space")

	conn := appConn(t)
	ctx := context.Background()
	actAs(t, conn, home.org, home.user)
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(`SELECT count(*) FROM page_task WHERE page_id = $1`, farPage) != 1 {
		t.Fatal("the owner reads no task of TFAR, so the token's zero would prove nothing")
	}
	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, db.TokenSpacesVar, "{"+nearID+"}"); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM page_task WHERE page_id = $1`, farPage); n != 0 {
		t.Errorf("the token reads %d tasks of TFAR", n)
	}
	untouched(t, conn, "ticking TFAR's task", `UPDATE page_task SET done = true WHERE page_id = $1`, farPage)
	denied(t, conn, "a task planted in TFAR", `INSERT INTO page_task (org_id, page_id, task_id, position, summary) VALUES ($1, $2, $3, 1, 'Planted')`, home.org, farPage, uuid.New())
	if _, err := conn.Exec(ctx, `SELECT set_config($1, '', false)`, db.TokenSpacesVar); err != nil {
		t.Fatal(err)
	}
}
