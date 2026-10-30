package perm

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// The access inspector (#81) decides nothing itself: every verdict and every
// step's outcome is what the SQL functions behind the policies answer, so the
// explanation cannot drift from what the database enforces.

// Right is something a person may do to a page, as the inspector names it.
type Right string

const (
	RightView    Right = "view"
	RightEdit    Right = "edit"
	RightDelete  Right = "delete"
	RightComment Right = "comment"
)

// Rights lists every Right in the order the inspector answers them.
var Rights = []Right{RightView, RightEdit, RightDelete, RightComment}

// StepKind is one condition a right depends on.
type StepKind string

const (
	// StepUse is the installation's use permission.
	StepUse StepKind = "use"
	// StepSpace is holding a space permission.
	StepSpace StepKind = "space"
	// StepUnpublished is a page on the way up that nobody but its author sees yet.
	StepUnpublished StepKind = "unpublished"
	// StepList is a view or edit list on the page or a page above it.
	StepList StepKind = "list"
	// StepGrant is a grant list on the page or a page above it that names
	// the person, which lets them edit without add pages in the space.
	StepGrant StepKind = "grant"
	// StepView is viewing the page, which every other right needs first.
	StepView StepKind = "view"
	// StepPublished is the page being published, which comments wait for.
	StepPublished StepKind = "published"
	// StepHome is the page being a space's home page, which never goes to the trash.
	StepHome StepKind = "home"
	// StepArchived is the page or its space being archived, which nothing
	// changes until it is unarchived; its page is the page archived, if any.
	StepArchived StepKind = "archived"
)

// StepKinds lists every StepKind, for the API document.
var StepKinds = []StepKind{StepUse, StepSpace, StepUnpublished, StepList, StepGrant, StepView, StepPublished, StepHome, StepArchived}

// ListKind is which of a page's lists a restriction is on.
type ListKind string

const (
	ListView ListKind = "view"
	ListEdit ListKind = "edit"
	// ListEditGrant is the list of who else may edit, without add pages.
	ListEditGrant ListKind = "editGrant"
)

// ListKinds lists every ListKind, for the API document.
var ListKinds = []ListKind{ListView, ListEdit, ListEditGrant}

// AccessPage is a page a step is about.
type AccessPage struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Home  bool      `json:"home"`
}

// AccessStep is one condition, whether the person meets it, and why.
type AccessStep struct {
	Kind   StepKind `json:"kind"`
	Passed bool     `json:"passed"`
	// Permission is the space permission a space step asks for.
	Permission *SpacePermission `json:"permission,omitempty"`
	// List and Page say which list a list step is, and an unpublished step's page.
	List *ListKind   `json:"list,omitempty"`
	Page *AccessPage `json:"page,omitempty"`
	// Bypassed is a list the person is not on but passes as an administrator.
	Bypassed bool `json:"bypassed"`
	// Via are the subjects of a list that name the person.
	Via []Subject `json:"via"`
	// Grants are the grants that reach the person and give the permission a
	// use or space step asks for.
	Grants []AccessGrant `json:"grants"`
	// Listed is everybody a list step's list names.
	Listed []Subject `json:"listed"`
}

// AccessRight is one right, the database's verdict and the steps behind it;
// the first step not passed is the one that decides a no.
type AccessRight struct {
	Right   Right        `json:"right"`
	Allowed bool         `json:"allowed"`
	Steps   []AccessStep `json:"steps"`
}

// AccessReport is what one person may do to one page, and why.
type AccessReport struct {
	Person Person `json:"person"`
	// Role is the person's standing, member or guest.
	Role   auth.Role     `json:"role"`
	Rights []AccessRight `json:"rights"`
}

// AccessGrant is a grant that reaches the person, in the space or across the
// installation, with the permissions its role holds.
type AccessGrant struct {
	Assignment
	Permissions []Permission `json:"permissions"`
}

// ListFacts is one list on one page as it concerns the person.
type ListFacts struct {
	// Listed is everybody on the list; empty is no list.
	Listed []Subject
	// On is whether the person passes it, and Via the entries that let them.
	On  bool
	Via []Subject
}

// InspectLink is one page on the way from the page up to its home page.
type InspectLink struct {
	Page AccessPage
	// Hidden is a page nobody has published yet, made by somebody else.
	Hidden            bool
	View, Edit, Grant ListFacts
}

// InspectFacts are the database's answers for one person and one page.
type InspectFacts struct {
	Use bool
	// Space is perm_space_holds for each space permission.
	Space map[SpacePermission]bool
	// Grants are the grants that reach the person in the space or across the
	// installation, before implication.
	Grants []AccessGrant
	// Chain is the page and every page above it, the home page first.
	Chain     []InspectLink
	Published bool
	// Trashable is page_trashable, false for a space's home page.
	Trashable bool
	// Archived says whether the page or its space is archived; ArchivedWith
	// is the page archived, this one or one above it.
	Archived     Archived
	ArchivedWith *AccessPage
	// Verdict is the database's own answer for each right.
	Verdict map[Right]bool
}

// Explain lays the facts out as the steps of each right.
func Explain(f InspectFacts) []AccessRight {
	view := AccessRight{Right: RightView, Allowed: f.Verdict[RightView]}
	view.Steps = append(view.Steps, f.standing(SpaceView, true)...)
	for _, l := range f.Chain {
		if l.Hidden {
			page := l.Page
			view.Steps = append(view.Steps, step(AccessStep{Kind: StepUnpublished, Page: &page}))
		}
	}
	view.Steps = append(view.Steps, f.lists(ListView)...)

	viewed := step(AccessStep{Kind: StepView, Passed: f.Verdict[RightView]})
	var frozen []AccessStep
	if f.Archived != NotArchived {
		frozen = append(frozen, step(AccessStep{Kind: StepArchived, Page: f.ArchivedWith}))
	}
	out := []AccessRight{view}
	for _, r := range []struct {
		right Right
		needs SpacePermission
	}{{RightEdit, SpaceAddPages}, {RightDelete, SpaceDelete}} {
		a := AccessRight{Right: r.right, Allowed: f.Verdict[r.right], Steps: append([]AccessStep{viewed}, frozen...)}
		if r.right == RightDelete && !f.Trashable {
			a.Steps = append(a.Steps, step(AccessStep{Kind: StepHome}))
		}
		if r.right == RightEdit && f.grantDecides() {
			a.Steps = append(a.Steps, f.grants()...)
		} else {
			a.Steps = append(a.Steps, f.standing(r.needs, false)...)
		}
		a.Steps = append(a.Steps, f.lists(ListEdit)...)
		out = append(out, a)
	}
	comment := AccessRight{Right: RightComment, Allowed: f.Verdict[RightComment], Steps: append([]AccessStep{viewed}, frozen...)}
	if !f.Published {
		comment.Steps = append(comment.Steps, step(AccessStep{Kind: StepPublished}))
	}
	comment.Steps = append(comment.Steps, f.standing(SpaceAddComments, false)...)
	return append(out, comment)
}

// step fills the lists an answer always carries.
func step(s AccessStep) AccessStep {
	s.Via, s.Grants, s.Listed = nonNil(s.Via), nonNil(s.Grants), nonNil(s.Listed)
	return s
}

// standing is how the person holds a space permission: through use and the
// grants that reach them.
func (f InspectFacts) standing(p SpacePermission, withUse bool) []AccessStep {
	var out []AccessStep
	if withUse {
		out = append(out, step(AccessStep{Kind: StepUse, Passed: f.Use, Grants: grantsHolding(f.Grants, Permission(UseStator))}))
		// Without use the space's grants count for nothing, so showing them would mislead.
		if !f.Use {
			return out
		}
	}
	return append(out, f.spaceStep(p))
}

func (f InspectFacts) spaceStep(p SpacePermission) AccessStep {
	return step(AccessStep{Kind: StepSpace, Permission: &p, Passed: f.Space[p], Grants: grantsGiving(f.Grants, p)})
}

// grantsGiving keeps the grants whose role gives p: p itself, administer, or
// any space permission at all for view.
func grantsGiving(grants []AccessGrant, p SpacePermission) []AccessGrant {
	var out []AccessGrant
	for _, g := range grants {
		if slices.ContainsFunc(g.Permissions, func(held Permission) bool {
			return held == Permission(p) || held == Permission(SpaceAdminister) ||
				(p == SpaceView && slices.Contains(SpacePermissions, SpacePermission(held)))
		}) {
			out = append(out, g)
		}
	}
	return out
}

// grantsHolding keeps the grants across the installation whose role holds p.
func grantsHolding(grants []AccessGrant, p Permission) []AccessGrant {
	var out []AccessGrant
	for _, g := range grants {
		if g.SpaceID == nil && slices.Contains(g.Permissions, p) {
			out = append(out, g)
		}
	}
	return out
}

// grantDecides says a grant list, not the space's add pages, is what lets
// the person edit, or would once the other conditions are met.
func (f InspectFacts) grantDecides() bool {
	if f.Space[SpaceAddPages] || !f.Use {
		return false
	}
	for _, l := range f.Chain {
		if l.Grant.On {
			return true
		}
	}
	return false
}

// grants are the steps of the grant lists on the way up that name the person.
func (f InspectFacts) grants() []AccessStep {
	var out []AccessStep
	for _, l := range f.Chain {
		if !l.Grant.On {
			continue
		}
		page, k := l.Page, ListEditGrant
		out = append(out, step(AccessStep{Kind: StepGrant, List: &k, Page: &page, Passed: true, Via: l.Grant.Via, Listed: l.Grant.Listed}))
	}
	return out
}

// lists are the steps of every list of a kind on the way up, which an
// administrator of the space passes; the grant that makes them one comes first.
// A page's grant list passes its own edit list too.
func (f InspectFacts) lists(kind ListKind) []AccessStep {
	admin := f.Space[SpaceAdminister]
	var out []AccessStep
	for _, l := range f.Chain {
		facts := l.View
		if kind == ListEdit {
			facts = l.Edit
			facts.On = facts.On || l.Grant.On
			facts.Via = append(slices.Clone(facts.Via), l.Grant.Via...)
		}
		if len(facts.Listed) == 0 {
			continue
		}
		if len(out) == 0 && admin {
			out = append(out, f.spaceStep(SpaceAdminister))
		}
		page, k := l.Page, kind
		out = append(out, step(AccessStep{Kind: StepList, List: &k, Page: &page, Passed: facts.On || admin,
			Bypassed: admin && !facts.On, Via: facts.Via, Listed: facts.Listed}))
	}
	return out
}

// Inspect explains what the database answers for person on page, once the
// caller has checked the inspector may ask; a non-member is auth.ErrNoSuchMember.
func Inspect(ctx context.Context, tx db.DBTX, person, page uuid.UUID) (*AccessReport, error) {
	report := &AccessReport{}
	err := tx.QueryRow(ctx, `
		SELECT u.id, u.name, u.email::text, m.role
		FROM member m JOIN app_user u ON u.id = m.user_id
		WHERE m.user_id = $1`, person).Scan(
		&report.Person.ID, &report.Person.Name, &report.Person.Email, &report.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.ErrNoSuchMember
	}
	if err != nil {
		return nil, fmt.Errorf("read the person: %w", err)
	}
	f, err := inspectFacts(ctx, tx, person, page)
	if err != nil {
		return nil, err
	}
	report.Rights = Explain(f)
	return report, nil
}

func inspectFacts(ctx context.Context, tx db.DBTX, person, page uuid.UUID) (InspectFacts, error) {
	f := InspectFacts{Space: map[SpacePermission]bool{}, Verdict: map[Right]bool{}}
	rows, err := tx.Query(ctx, `
		SELECT l.id, l.space_id, COALESCE(p.title, ''), COALESCE(p.parent_id IS NULL AND p.kind <> 'post', false), COALESCE(p.version > 0, false),
		       l.hidden, l.on_view_list, l.on_edit_list, l.granted
		FROM perm_page_lists($1, $2) WITH ORDINALITY AS l (id, space_id, hidden, view_listed, on_view_list, edit_listed, on_edit_list, granted, n)
		LEFT JOIN page p ON p.id = l.id
		ORDER BY l.n DESC`, page, person)
	if err != nil {
		return f, fmt.Errorf("read the pages above: %w", err)
	}
	var (
		space     uuid.UUID
		published []bool
	)
	f.Chain, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (InspectLink, error) {
		var (
			l  InspectLink
			pb bool
		)
		err := row.Scan(&l.Page.ID, &space, &l.Page.Title, &l.Page.Home, &pb, &l.Hidden, &l.View.On, &l.Edit.On, &l.Grant.On)
		published = append(published, pb)
		return l, err
	})
	if err != nil {
		return f, fmt.Errorf("read the pages above: %w", err)
	}
	if len(f.Chain) == 0 {
		return f, nil
	}
	f.Published = published[len(published)-1]

	var view, edit, del, comment bool
	held := make([]bool, len(SpacePermissions))
	err = tx.QueryRow(ctx, `
		SELECT perm_global_holds($1, 'use'),
		       perm_space_holds($1, $3, 'view'), perm_space_holds($1, $3, 'addPages'), perm_space_holds($1, $3, 'addComments'),
		       perm_space_holds($1, $3, 'delete'), perm_space_holds($1, $3, 'administer'),
		       perm_page_viewable($2, $1), perm_page_editable($2, $1), perm_page_deletable($2, $1), perm_page_commentable($2, $1),
		       COALESCE((SELECT page_trashable(parent_id, kind) FROM page WHERE id = $2), false)`,
		person, page, space).Scan(&f.Use, &held[0], &held[1], &held[2], &held[3], &held[4], &view, &edit, &del, &comment, &f.Trashable)
	if err != nil {
		return f, fmt.Errorf("ask the database: %w", err)
	}
	for i, p := range SpacePermissions {
		f.Space[p] = held[i]
	}
	var (
		with   AccessPage
		withID *uuid.UUID
	)
	err = tx.QueryRow(ctx, `
		SELECT CASE WHEN s.archived_at IS NOT NULL THEN 'space' WHEN p.archived_at IS NOT NULL THEN 'page' ELSE '' END,
		       a.id, COALESCE(a.title, ''), COALESCE(a.parent_id IS NULL AND a.kind <> 'post', false)
		FROM page p JOIN space s ON s.id = p.space_id LEFT JOIN page a ON a.id = p.archive_id
		WHERE p.id = $1`, page).Scan(&f.Archived, &withID, &with.Title, &with.Home)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return f, fmt.Errorf("read whether the page is archived: %w", err)
	}
	if withID != nil {
		with.ID = *withID
		f.ArchivedWith = &with
	}
	f.Verdict = map[Right]bool{RightView: view, RightEdit: edit, RightDelete: del && f.Trashable, RightComment: comment}

	if f.Grants, err = reachingGrants(ctx, tx, person, space); err != nil {
		return f, err
	}
	return f, readLists(ctx, tx, person, f.Chain)
}

// reachingGrants are the grants that reach the person in the space or across
// the installation, the installation's first.
func reachingGrants(ctx context.Context, tx db.DBTX, person, space uuid.UUID) ([]AccessGrant, error) {
	rows, err := tx.Query(ctx, `
		SELECT a.id, a.role, r.name, a.space_id, COALESCE(s.key, ''), COALESCE(s.name, ''),
		       a.user_id, COALESCE(u.name, ''), COALESCE(u.email::text, ''), false,
		       a.group_id, COALESCE(g.name, ''), COALESCE(g.builtin, ''), a.created_at, r.permissions
		FROM perm_grants_reaching($1, $2) a
		JOIN role r ON r.key = a.role
		LEFT JOIN space s ON s.id = a.space_id
		LEFT JOIN app_user u ON u.id = a.user_id
		LEFT JOIN groups g ON g.id = a.group_id
		ORDER BY a.space_id IS NOT NULL, r.sort_order, lower(r.name), a.group_id IS NULL, a.id`, person, space)
	if err != nil {
		return nil, fmt.Errorf("read the grants that reach the person: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (AccessGrant, error) {
		var (
			g     AccessGrant
			perms []string
		)
		err := row.Scan(&g.ID, &g.Role, &g.RoleName, &g.SpaceID, &g.SpaceKey, &g.SpaceName,
			&g.UserID, &g.UserName, &g.UserEmail, &g.Guest, &g.GroupID, &g.GroupName, &g.GroupBuiltin, &g.CreatedAt, &perms)
		for _, p := range AllPermissions {
			if slices.Contains(perms, string(p)) {
				g.Permissions = append(g.Permissions, p)
			}
		}
		return g, err
	})
}

func sameSubject(a, b Subject) bool {
	return a.Type == b.Type && (a.ID == nil) == (b.ID == nil) && (a.ID == nil || *a.ID == *b.ID)
}

// readLists fills in who each list on the way up names, and which of them
// name the person.
func readLists(ctx context.Context, tx db.DBTX, person uuid.UUID, chain []InspectLink) error {
	ids := make([]uuid.UUID, len(chain))
	for i, l := range chain {
		ids[i] = l.Page.ID
	}
	rows, err := tx.Query(ctx, `SELECT g.page_id, g.kind, `+SubjectColumns+`,
		       COALESCE(perm_subject_matches($1, g.subject_type, g.user_id, g.group_id), false)
		FROM page_restriction g`+SubjectJoins+`
		WHERE g.page_id = ANY ($2) ORDER BY `+SubjectOrder, person, ids)
	if err != nil {
		return fmt.Errorf("read the restrictions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			pageID  uuid.UUID
			kind    ListKind
			sub     Subject
			matches bool
		)
		if err := rows.Scan(&pageID, &kind, &sub.Type, &sub.ID, &sub.Name, &matches); err != nil {
			return err
		}
		i := slices.IndexFunc(chain, func(l InspectLink) bool { return l.Page.ID == pageID })
		if i < 0 {
			continue
		}
		list := &chain[i].View
		switch kind {
		case ListEdit:
			list = &chain[i].Edit
		case ListEditGrant:
			list = &chain[i].Grant
		}
		list.Listed = append(list.Listed, sub)
		if matches {
			list.Via = append(list.Via, sub)
		}
	}
	return rows.Err()
}
