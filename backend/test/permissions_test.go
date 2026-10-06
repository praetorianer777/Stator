//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// makeGroup makes a group of the organization with members, behind the
// policies' back like makeMember.
func (h *harness) makeGroup(t *testing.T, org uuid.UUID, name string, members ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, org, name).Scan(&id); err != nil {
		t.Fatalf("make the group %s: %v", name, err)
	}
	for _, m := range members {
		if _, err := h.super.Exec(ctx, `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, org, id, m); err != nil {
			t.Fatalf("put a member in %s: %v", name, err)
		}
	}
	h.settle(t)
	return id
}

func user(id uuid.UUID) map[string]any  { return map[string]any{"type": "user", "id": id} }
func group(id uuid.UUID) map[string]any { return map[string]any{"type": "group", "id": id} }

var everyone = map[string]any{"type": "everyone"}

// restrict replaces a page's own lists.
func restrict(t *testing.T, c *client, page string, view, edit []any) response {
	t.Helper()
	if view == nil {
		view = []any{}
	}
	if edit == nil {
		edit = []any{}
	}
	return c.put(t, pagePath(page, "/restrictions"), map[string]any{"view": view, "edit": edit})
}

// The permission matrix: global grants, space grants and page restrictions
// decide what each person is answered, and every change is audited.
func TestPermissionsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "perms")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, bobID, carlID, daveID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, bob, carl, dave := api.as(t, annID, home.org, slug), api.as(t, bobID, home.org, slug), api.as(t, carlID, home.org, slug), api.as(t, daveID, home.org, slug)
	writers := h.makeGroup(t, home.org, "Writers", bobID)

	t.Run("global permissions", func(t *testing.T) {
		me := obj(t, want(t, ann.get(t, "/api/v1/access/me"), http.StatusOK, "ann's access"), "can")
		if me["use"] != true || me["createSpace"] != false || me["administer"] != false {
			t.Errorf("a member may %v", me)
		}
		me = obj(t, want(t, owner.get(t, "/api/v1/access/me"), http.StatusOK, "the owner's access"), "can")
		if me["use"] != true || me["createSpace"] != true || me["administer"] != true {
			t.Errorf("the owner may %v", me)
		}
		if got := api.anonymous().get(t, "/api/v1/access/me"); got.Status != http.StatusUnauthorized {
			t.Errorf("nobody's access: %d", got.Status)
		}

		perms := list(t, want(t, owner.get(t, "/api/v1/org/permissions"), http.StatusOK, "global permissions"), "permissions")
		if len(perms) != 3 {
			t.Fatalf("%d global permissions", len(perms))
		}
		use, admin := perms[0].(map[string]any), perms[2].(map[string]any)
		if use["permission"] != "use" || len(use["subjects"].([]any)) != 1 || use["subjects"].([]any)[0].(map[string]any)["type"] != "everyone" {
			t.Errorf("use is granted to %v", use)
		}
		if admin["permission"] != "administer" || admin["fixed"] != true || len(admin["subjects"].([]any)) != 1 {
			t.Errorf("administer is %v", admin)
		}
		if got := ann.get(t, "/api/v1/org/permissions"); got.Status != http.StatusForbidden {
			t.Errorf("a member reads the global permissions: %d", got.Status)
		}
		if got := owner.put(t, "/api/v1/org/permissions/administer", map[string]any{"subjects": []any{user(annID)}}); got.Status != http.StatusConflict {
			t.Errorf("administer was granted: %d", got.Status)
		}
		if got := ann.put(t, "/api/v1/org/permissions/createSpace", map[string]any{"subjects": []any{user(annID)}}); got.Status != http.StatusForbidden {
			t.Errorf("a member granted themselves createSpace: %d", got.Status)
		}

		if got := ann.post(t, "/api/v1/spaces", map[string]any{"key": "ANNS", "name": "Ann's"}); got.Status != http.StatusForbidden {
			t.Fatalf("a member made a space: %d", got.Status)
		}
		granted := obj(t, want(t, owner.put(t, "/api/v1/org/permissions/createSpace", map[string]any{"subjects": []any{user(annID), user(annID)}}), http.StatusOK, "grant createSpace"), "permission")
		if subjects := granted["subjects"].([]any); len(subjects) != 1 || subjects[0].(map[string]any)["name"] != "A member" {
			t.Errorf("createSpace is granted to %v", subjects)
		}
		sp := obj(t, want(t, ann.post(t, "/api/v1/spaces", map[string]any{"key": "ANNS", "name": "Ann's"}), http.StatusCreated, "ann's space"), "space")
		if can := sp["can"].(map[string]any); can["administer"] != true || can["purgeTrash"] != true {
			t.Errorf("the creator of a space may %v in it", can)
		}
		if got := bob.patch(t, "/api/v1/spaces/ANNS", map[string]any{"name": "Bob's"}); got.Status != http.StatusForbidden {
			t.Errorf("somebody else renamed ann's space: %d", got.Status)
		}

		want(t, owner.put(t, "/api/v1/org/permissions/use", map[string]any{"subjects": []any{group(writers)}}), http.StatusOK, "use for writers only")
		got := ann.get(t, "/api/v1/spaces")
		if got.Status != http.StatusForbidden || errorCode(t, got) != "no_access" {
			t.Errorf("a member without use lists spaces: %d %s", got.Status, got.Raw)
		}
		if me := obj(t, want(t, ann.get(t, "/api/v1/access/me"), http.StatusOK, "access without use"), "can"); me["use"] != false || me["createSpace"] != false {
			t.Errorf("without use a member may %v", me)
		}
		want(t, bob.get(t, "/api/v1/spaces"), http.StatusOK, "a writer still uses Stator")
		want(t, owner.get(t, "/api/v1/spaces"), http.StatusOK, "an owner always uses Stator")
		want(t, owner.put(t, "/api/v1/org/permissions/use", map[string]any{"subjects": []any{everyone}}), http.StatusOK, "use for everyone again")
		want(t, ann.get(t, "/api/v1/spaces"), http.StatusOK, "use is back")
	})

	t.Run("space permissions", func(t *testing.T) {
		sec := newTree(t, owner, "SEC", "Secure")
		page := sec.add(sec.homeID, "Plans")
		grants := list(t, want(t, owner.get(t, "/api/v1/spaces/SEC/permissions"), http.StatusOK, "the space's table"), "grants")
		if len(grants) != 2 {
			t.Fatalf("a new space grants %v", grants)
		}
		first, second := grants[0].(map[string]any), grants[1].(map[string]any)
		if first["subject"].(map[string]any)["type"] != "everyone" || len(first["permissions"].([]any)) != 4 {
			t.Errorf("everyone holds %v", first)
		}
		if second["subject"].(map[string]any)["type"] != "user" || second["permissions"].([]any)[0] != "administer" {
			t.Errorf("the creator holds %v", second)
		}
		if got := ann.get(t, "/api/v1/spaces/SEC/permissions"); got.Status != http.StatusForbidden {
			t.Errorf("a member read the space's table: %d", got.Status)
		}
		if got := ann.put(t, "/api/v1/spaces/SEC/permissions", map[string]any{"grants": []any{}}); got.Status != http.StatusForbidden {
			t.Errorf("a member replaced the space's table: %d", got.Status)
		}
		for what, body := range map[string]any{
			"a row with no permissions": []any{map[string]any{"subject": everyone, "permissions": []any{}}},
			"an unknown permission":     []any{map[string]any{"subject": everyone, "permissions": []any{"fly"}}},
			"an unknown person":         []any{map[string]any{"subject": user(uuid.New()), "permissions": []any{"view"}}},
		} {
			if got := owner.put(t, "/api/v1/spaces/SEC/permissions", map[string]any{"grants": body}); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d", what, got.Status)
			}
		}

		table := want(t, owner.put(t, "/api/v1/spaces/SEC/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
			map[string]any{"subject": group(writers), "permissions": []any{"addPages", "addComments"}},
			map[string]any{"subject": user(daveID), "permissions": []any{"administer"}},
		}}), http.StatusOK, "narrow the space")
		if n := len(list(t, table, "grants")); n != 3 {
			t.Errorf("the table has %d rows", n)
		}
		readerCan := obj(t, want(t, ann.get(t, "/api/v1/spaces/SEC"), http.StatusOK, "a reader's space"), "space", "can")
		if readerCan["editPages"] != false || readerCan["deletePages"] != false || readerCan["addComments"] != false {
			t.Errorf("a reader may %v", readerCan)
		}
		if got := ann.patch(t, "/api/v1/pages/"+page, map[string]any{"title": "Mine", "version": 1}); got.Status != http.StatusForbidden {
			t.Errorf("a reader edited a page: %d", got.Status)
		}
		if got := ann.delete(t, "/api/v1/pages/"+page); got.Status != http.StatusForbidden {
			t.Errorf("a reader trashed a page: %d", got.Status)
		}
		pg := obj(t, want(t, bob.patch(t, "/api/v1/pages/"+page, map[string]any{"title": "Plans, revised", "version": 1}), http.StatusOK, "a writer edits"), "page")
		if can := pg["can"].(map[string]any); can["edit"] != true || can["delete"] != false || can["comment"] != true {
			t.Errorf("a writer may %v", can)
		}
		if got := bob.delete(t, "/api/v1/pages/"+page); got.Status != http.StatusForbidden {
			t.Errorf("a writer without delete trashed a page: %d", got.Status)
		}
		want(t, dave.get(t, "/api/v1/spaces/SEC/permissions"), http.StatusOK, "a space administrator reads the table")

		// A space administrator who is no organization administrator may give
		// the space away, though not to nobody, and the answer is still the
		// table they wrote.
		fieldError(t, want(t, dave.put(t, "/api/v1/spaces/SEC/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments", "delete"}},
		}}), http.StatusUnprocessableEntity, "dave leaves the space without an administrator"), "grants")
		given := want(t, dave.put(t, "/api/v1/spaces/SEC/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments", "delete"}},
			map[string]any{"subject": group(writers), "permissions": []any{"administer"}},
		}}), http.StatusOK, "dave gives the space away")
		if n := len(list(t, given, "grants")); n != 2 {
			t.Errorf("the table dave wrote has %d rows", n)
		}
		if got := dave.get(t, "/api/v1/spaces/SEC/permissions"); got.Status != http.StatusForbidden {
			t.Errorf("dave still reads the table: %d", got.Status)
		}
		want(t, owner.get(t, "/api/v1/spaces/SEC/permissions"), http.StatusOK, "no space is ever orphaned")
		want(t, carl.post(t, "/api/v1/pages", map[string]any{"parentId": sec.homeID, "title": "Open again", "publish": true}), http.StatusCreated, "the space is open again")

		// No view for anybody but the administrators hides the space itself.
		want(t, owner.put(t, "/api/v1/spaces/SEC/permissions", map[string]any{"grants": []any{}}), http.StatusOK, "close the space")
		if got := ann.get(t, "/api/v1/spaces/SEC"); got.Status != http.StatusNotFound {
			t.Errorf("a closed space answers %d", got.Status)
		}
		for _, each := range list(t, want(t, ann.get(t, "/api/v1/spaces"), http.StatusOK, "list spaces"), "spaces") {
			if each.(map[string]any)["key"] == "SEC" {
				t.Error("a closed space is listed")
			}
		}
		if got := ann.get(t, "/api/v1/pages/"+page); got.Status != http.StatusNotFound {
			t.Errorf("a page of a closed space answers %d", got.Status)
		}
	})

	t.Run("page restrictions", func(t *testing.T) {
		docs := newTree(t, owner, "RES", "Restricted")
		want(t, owner.put(t, "/api/v1/spaces/RES/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments", "delete"}},
			map[string]any{"subject": user(daveID), "permissions": []any{"administer"}},
		}}), http.StatusOK, "dave administers RES")
		top := docs.add(docs.homeID, "Top")
		mid := docs.add(top, "Mid")
		low := docs.add(mid, "Low")
		docs.add(docs.homeID, "Open")

		saved := obj(t, want(t, restrict(t, ann, top, []any{user(annID), group(writers)}, nil), http.StatusOK, "ann hides Top"), "restrictions")
		if view := saved["view"].([]any); len(view) != 2 || view[0].(map[string]any)["name"] != "Writers" {
			t.Errorf("Top's view list is %v", view)
		}
		if got := carl.get(t, "/api/v1/pages/"+low); got.Status != http.StatusNotFound {
			t.Errorf("a page below a view list answers carl %d", got.Status)
		}
		for what, got := range map[string]response{
			"page":         carl.get(t, "/api/v1/pages/"+top),
			"history":      carl.get(t, pagePath(top, "/versions")),
			"version":      carl.get(t, pagePath(top, "/versions/1")),
			"compare":      carl.get(t, pagePath(top, "/compare")),
			"draft":        carl.get(t, pagePath(top, "/draft")),
			"restrictions": carl.get(t, pagePath(top, "/restrictions")),
			"children":     carl.get(t, "/api/v1/spaces/RES/pages?parent="+top),
			"edit":         carl.patch(t, "/api/v1/pages/"+mid, map[string]any{"title": "Mine", "version": 1}),
			"copy":         carl.post(t, "/api/v1/pages/"+mid+"/copy", map[string]any{"parentId": docs.homeID}),
			"move":         carl.post(t, "/api/v1/pages/"+mid+"/move", map[string]any{"parentId": docs.homeID}),
			"add under it": carl.post(t, "/api/v1/pages", map[string]any{"parentId": low, "title": "Planted"}),
			"trash":        carl.delete(t, "/api/v1/pages/"+low),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("carl's %s of a hidden page: %d", what, got.Status)
			}
		}
		carlTree := &tree{t: t, c: carl, key: "RES", homeID: docs.homeID}
		sameTitles(t, "carl's tree", carlTree.titles(""), "Open")
		for _, e := range list(t, want(t, carl.get(t, "/api/v1/spaces/RES/outline"), http.StatusOK, "carl's outline"), "pages") {
			if title := e.(map[string]any)["title"]; title == "Top" || title == "Mid" || title == "Low" {
				t.Errorf("carl's outline shows %s", title)
			}
		}

		bobTree := &tree{t: t, c: bob, key: "RES", homeID: docs.homeID}
		sameTitles(t, "bob's tree", bobTree.titles(""), "Top", "Open")
		for _, n := range list(t, want(t, bob.get(t, "/api/v1/spaces/RES/pages"), http.StatusOK, "bob's tree"), "pages") {
			node := n.(map[string]any)
			if want := node["title"] == "Top"; node["restricted"] != want {
				t.Errorf("%s is restricted %v", node["title"], node["restricted"])
			}
		}
		for _, n := range list(t, want(t, bob.get(t, "/api/v1/spaces/RES/pages?parent="+top), http.StatusOK, "under Top"), "pages") {
			if n.(map[string]any)["restricted"] != true {
				t.Errorf("Mid does not inherit Top's restriction: %v", n)
			}
		}

		want(t, restrict(t, ann, mid, nil, []any{user(annID)}), http.StatusOK, "ann alone edits Mid")
		lowPage := obj(t, want(t, bob.get(t, "/api/v1/pages/"+low), http.StatusOK, "bob reads Low"), "page")
		if can, r := lowPage["can"].(map[string]any), lowPage["restricted"].(map[string]any); can["edit"] != false || can["restrict"] != false || can["delete"] != false || can["comment"] != true || r["view"] != true || r["edit"] != true {
			t.Errorf("bob may %v on Low, restricted %v", can, r)
		}
		if got := bob.patch(t, "/api/v1/pages/"+low, map[string]any{"title": "Bob's", "version": 1}); got.Status != http.StatusForbidden {
			t.Errorf("bob edited below an edit list: %d", got.Status)
		}
		if got := bob.put(t, pagePath(low, "/draft"), map[string]any{"title": "Bob's", "body": textDoc("x"), "baseVersion": 1}); got.Status != http.StatusForbidden {
			t.Errorf("bob drafted below an edit list: %d", got.Status)
		}
		if got := restrict(t, bob, low, nil, nil); got.Status != http.StatusForbidden {
			t.Errorf("bob restricted below an edit list: %d", got.Status)
		}
		want(t, ann.patch(t, "/api/v1/pages/"+low, map[string]any{"title": "Low, by ann", "version": 1}), http.StatusOK, "ann edits Low")

		inherited := obj(t, want(t, bob.get(t, pagePath(low, "/restrictions")), http.StatusOK, "why Low is restricted"), "restrictions")
		if above := inherited["inherited"].([]any); len(above) != 2 || above[0].(map[string]any)["page"].(map[string]any)["title"] != "Top" || above[1].(map[string]any)["page"].(map[string]any)["title"] != "Mid" {
			t.Errorf("Low inherits %v", above)
		}

		file := obj(t, want(t, ann.upload(t, pagePath(low, "/attachments"), "plan.txt", []byte("secret")), http.StatusCreated, "ann attaches to Low"), "attachment")["id"].(string)
		if got := bob.upload(t, pagePath(low, "/attachments"), "bob.txt", []byte("x")); got.Status != http.StatusForbidden {
			t.Errorf("bob attached below an edit list: %d", got.Status)
		}
		if got := bob.delete(t, "/api/v1/attachments/"+file); got.Status != http.StatusForbidden {
			t.Errorf("bob removed a file below an edit list: %d", got.Status)
		}
		if n := len(list(t, want(t, bob.get(t, pagePath(low, "/attachments")), http.StatusOK, "bob lists Low's files"), "attachments")); n != 1 {
			t.Errorf("bob sees %d of Low's files", n)
		}
		if got := carl.get(t, pagePath(low, "/attachments")); got.Status != http.StatusNotFound {
			t.Errorf("carl lists a hidden page's files: %d", got.Status)
		}
		if resp, _ := carl.download(t, "/api/v1/attachments/"+file); resp.StatusCode != http.StatusNotFound {
			t.Errorf("carl downloads a hidden page's file: %d", resp.StatusCode)
		}

		lockout := restrict(t, ann, low, []any{user(bobID)}, nil)
		if lockout.Status != http.StatusConflict || errorCode(t, lockout) != "conflict" {
			t.Errorf("a save that locks ann out: %d %s", lockout.Status, lockout.Raw)
		}
		if got := restrict(t, ann, low, nil, []any{user(bobID)}); got.Status != http.StatusConflict {
			t.Errorf("a save that takes ann's edit away: %d", got.Status)
		}
		if n := len(obj(t, want(t, ann.get(t, pagePath(low, "/restrictions")), http.StatusOK, "Low after the refusals"), "restrictions")["view"].([]any)); n != 0 {
			t.Errorf("a refused save left %d names on Low", n)
		}
		for what, got := range map[string]response{
			"a view list on the home page": restrict(t, owner, docs.homeID, []any{user(annID)}, nil),
			"everyone on a list":           restrict(t, ann, low, []any{everyone}, nil),
			"somebody unknown":             restrict(t, ann, low, []any{user(uuid.New())}, nil),
		} {
			if got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d", what, got.Status)
			}
		}
		want(t, restrict(t, owner, docs.homeID, nil, []any{user(annID)}), http.StatusOK, "the home page takes an edit list")
		want(t, restrict(t, owner, docs.homeID, nil, nil), http.StatusOK, "and loses it")

		for who, c := range map[string]*client{"the owner": owner, "a space administrator": dave} {
			pg := obj(t, want(t, c.get(t, "/api/v1/pages/"+low), http.StatusOK, who+" reads Low"), "page")
			if can := pg["can"].(map[string]any); can["edit"] != true || can["delete"] != true {
				t.Errorf("%s is bound by the restrictions: %v", who, can)
			}
			// An administrator may even save a list that leaves them off.
			want(t, restrict(t, c, low, []any{user(annID)}, nil), http.StatusOK, who+" narrows Low past themselves")
			want(t, c.get(t, "/api/v1/pages/"+low), http.StatusOK, who+" still reads Low")
			want(t, restrict(t, c, low, nil, nil), http.StatusOK, who+" lifts it")
		}

		copied := obj(t, want(t, ann.post(t, "/api/v1/pages/"+top+"/copy", map[string]any{"parentId": docs.homeID, "withChildren": true, "title": "Top, copied"}), http.StatusCreated, "ann copies Top"), "page")
		copyID := copied["id"].(string)
		if r := obj(t, want(t, ann.get(t, pagePath(copyID, "/restrictions")), http.StatusOK, "the copy's lists"), "restrictions"); len(r["view"].([]any)) != 2 {
			t.Errorf("the copy keeps %v of Top's view list", r["view"])
		}
		if got := carl.get(t, "/api/v1/pages/"+copyID); got.Status != http.StatusNotFound {
			t.Errorf("the copy of a hidden page is seen: %d", got.Status)
		}

		moved := docs.add(docs.homeID, "Moved in")
		want(t, ann.post(t, "/api/v1/pages/"+moved+"/move", map[string]any{"parentId": top}), http.StatusOK, "move under Top")
		if got := carl.get(t, "/api/v1/pages/"+moved); got.Status != http.StatusNotFound {
			t.Errorf("a page moved under a view list is still seen: %d", got.Status)
		}

		// A person in a group on the list sees what the group sees; a person
		// who leaves the group stops seeing it.
		want(t, bob.get(t, "/api/v1/pages/"+top), http.StatusOK, "bob sees Top through Writers")
		if _, err := h.super.Exec(context.Background(), `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, writers, bobID); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if got := bob.get(t, "/api/v1/pages/"+top); got.Status != http.StatusNotFound {
			t.Errorf("bob left Writers and still sees Top: %d", got.Status)
		}
		if _, err := h.super.Exec(context.Background(), `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, home.org, writers, bobID); err != nil {
			t.Fatal(err)
		}
		h.settle(t)

		trashed := docs.add(top, "Trashed secret")
		want(t, ann.delete(t, "/api/v1/pages/"+trashed), http.StatusNoContent, "ann trashes below Top")
		for _, item := range carlTree.trash() {
			if item["title"] == "Trashed secret" {
				t.Error("carl's trash lists a hidden page")
			}
		}
		if got := carlTree.restore(trashed); got.Status != http.StatusNotFound {
			t.Errorf("carl restored a hidden page: %d", got.Status)
		}
		annTree := &tree{t: t, c: ann, key: "RES", homeID: docs.homeID}
		found := false
		for _, item := range annTree.trash() {
			found = found || item["title"] == "Trashed secret"
		}
		if !found {
			t.Error("ann's trash does not list what she trashed")
		}
		want(t, annTree.restore(trashed), http.StatusOK, "ann restores it")
	})

	t.Run("pickers", func(t *testing.T) {
		people := list(t, want(t, ann.get(t, "/api/v1/people?q=member&limit=2"), http.StatusOK, "people"), "people")
		if len(people) != 2 {
			t.Errorf("the picker offers %d people, want the limit of 2", len(people))
		}
		if got := list(t, want(t, ann.get(t, "/api/v1/people?q=nobody-called-this"), http.StatusOK, "nobody"), "people"); len(got) != 0 {
			t.Errorf("the picker offers %v", got)
		}
		groups := list(t, want(t, ann.get(t, "/api/v1/groups?q=wri"), http.StatusOK, "groups"), "groups")
		if len(groups) != 1 || groups[0].(map[string]any)["name"] != "Writers" || groups[0].(map[string]any)["memberCount"].(float64) != 1 {
			t.Errorf("the group picker offers %v", groups)
		}
		for _, path := range []string{"/api/v1/people?limit=51", "/api/v1/groups?limit=0"} {
			if got := ann.get(t, path); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d", path, got.Status)
			}
		}
	})

	t.Run("every change is in the audit log", func(t *testing.T) {
		rows, err := h.super.Query(context.Background(), `SELECT DISTINCT action FROM audit_log WHERE org_id = $1 AND action LIKE '%permission%' OR org_id = $1 AND action LIKE '%restriction%'`, home.org)
		if err != nil {
			t.Fatal(err)
		}
		actions, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		for _, wanted := range []string{"org.permission_set", "space.permissions_set", "page.restrictions_set"} {
			found := false
			for _, a := range actions {
				found = found || a == wanted
			}
			if !found {
				t.Errorf("the audit log has no %s among %v", wanted, actions)
			}
		}
	})
}
