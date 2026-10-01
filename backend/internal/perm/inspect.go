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
	// StepOrgAdmin is administering the organization, which holds every
	// space permission and passes every list.
	StepOrgAdmin StepKind = "orgAdmin"
	// StepUse is the organization's use permission.
	StepUse StepKind = "use"
	// StepSpace is holding a space permission.
	StepSpace StepKind = "space"
	// StepUnpublished is a page on the way up that nobody but its author sees yet.
	StepUnpublished StepKind = "unpublished"
	// StepList is a view or edit list on the page or a page above it.
	StepList StepKind = "list"
	// StepView is viewing the page, which every other right needs first.
	StepView StepKind = "view"
	// StepPublished is the page being published, which comments wait for.
	StepPublished StepKind = "published"
)

// StepKinds lists every StepKind, for the API document.
var StepKinds = []StepKind{StepOrgAdmin, StepUse, StepSpace, StepUnpublished, StepList, StepView, StepPublished}

// ListKind is which of a page's two lists a restriction is on.
type ListKind string

const (
	ListView ListKind = "view"
	ListEdit ListKind = "edit"
)

// ListKinds lists every ListKind, for the API document.
var ListKinds = []ListKind{ListView, ListEdit}

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
	// Via are the subjects of the use grants, or of a list, that name the person.
	Via []Subject `json:"via"`
	// Grants are the space's grants that name the person and give the permission.
	Grants []SpaceGrant `json:"grants"`
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
	Person     Person          `json:"person"`
	Role       auth.OrgRole    `json:"role"`
	RoleSource auth.RoleSource `json:"roleSource"`
	Rights     []AccessRight   `json:"rights"`
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
	Hidden     bool
	View, Edit ListFacts
}

// InspectFacts are the database's answers for one person and one page.
type InspectFacts struct {
	OrgAdmin bool
	Use      bool
	UseVia   []Subject
	// Space is perm_space_holds for each space permission.
	Space map[SpacePermission]bool
	// Grants are the space's grants that name the person, before implication.
	Grants []SpaceGrant
	// Chain is the page and every page above it, the home page first.
	Chain     []InspectLink
	Published bool
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
	out := []AccessRight{view}
	for _, r := range []struct {
		right Right
		needs SpacePermission
	}{{RightEdit, SpaceAddPages}, {RightDelete, SpaceDelete}} {
		a := AccessRight{Right: r.right, Allowed: f.Verdict[r.right], Steps: []AccessStep{viewed}}
		a.Steps = append(a.Steps, f.standing(r.needs, false)...)
		a.Steps = append(a.Steps, f.lists(ListEdit)...)
		out = append(out, a)
	}
	comment := AccessRight{Right: RightComment, Allowed: f.Verdict[RightComment], Steps: []AccessStep{viewed}}
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

// standing is how the person holds a space permission: as an organization
// administrator, or through use and the space's grants.
func (f InspectFacts) standing(p SpacePermission, withUse bool) []AccessStep {
	if f.OrgAdmin {
		return []AccessStep{step(AccessStep{Kind: StepOrgAdmin, Passed: true})}
	}
	var out []AccessStep
	if withUse {
		out = append(out, step(AccessStep{Kind: StepUse, Passed: f.Use, Via: f.UseVia}))
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

// grantsGiving keeps the grants, and of each the permissions, that give p:
// p itself, administer, or anything at all for view.
func grantsGiving(grants []SpaceGrant, p SpacePermission) []SpaceGrant {
	var out []SpaceGrant
	for _, g := range grants {
		var giving []SpacePermission
		for _, held := range g.Permissions {
			if held == p || held == SpaceAdminister || p == SpaceView {
				giving = append(giving, held)
			}
		}
		if len(giving) > 0 {
			out = append(out, SpaceGrant{Subject: g.Subject, Permissions: giving})
		}
	}
	return out
}

// lists are the steps of every list of a kind on the way up, which an
// administrator of the space passes; the grant that makes them one comes first.
func (f InspectFacts) lists(kind ListKind) []AccessStep {
	admin := f.Space[SpaceAdminister]
	var out []AccessStep
	for _, l := range f.Chain {
		facts := l.View
		if kind == ListEdit {
			facts = l.Edit
		}
		if len(facts.Listed) == 0 {
			continue
		}
		if len(out) == 0 && admin && !f.OrgAdmin {
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
		SELECT u.id, u.name, u.email::text, m.org_role, m.role_source
		FROM org_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.org_id = current_org_id() AND m.user_id = $1`, person).Scan(
		&report.Person.ID, &report.Person.Name, &report.Person.Email, &report.Role, &report.RoleSource)
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
		SELECT l.id, l.space_id, COALESCE(p.title, ''), COALESCE(p.parent_id IS NULL, false), COALESCE(p.version > 0, false),
		       l.hidden, l.on_view_list, l.on_edit_list
		FROM perm_page_lists($1, $2) WITH ORDINALITY AS l (id, space_id, hidden, view_listed, on_view_list, edit_listed, on_edit_list, n)
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
		err := row.Scan(&l.Page.ID, &space, &l.Page.Title, &l.Page.Home, &pb, &l.Hidden, &l.View.On, &l.Edit.On)
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
		SELECT perm_is_admin($1), perm_global_holds($1, 'use'),
		       perm_space_holds($1, $3, 'view'), perm_space_holds($1, $3, 'addPages'), perm_space_holds($1, $3, 'addComments'),
		       perm_space_holds($1, $3, 'delete'), perm_space_holds($1, $3, 'administer'),
		       perm_page_viewable($2, $1), perm_page_editable($2, $1), perm_page_deletable($2, $1), perm_page_commentable($2, $1)`,
		person, page, space).Scan(&f.OrgAdmin, &f.Use, &held[0], &held[1], &held[2], &held[3], &held[4], &view, &edit, &del, &comment)
	if err != nil {
		return f, fmt.Errorf("ask the database: %w", err)
	}
	for i, p := range SpacePermissions {
		f.Space[p] = held[i]
	}
	f.Verdict = map[Right]bool{RightView: view, RightEdit: edit, RightDelete: del, RightComment: comment}

	if rows, err = tx.Query(ctx, `SELECT `+SubjectColumns+` FROM perm_global_grant_sources($1, 'use') g`+SubjectJoins+`
		ORDER BY `+SubjectOrder, person); err != nil {
		return f, fmt.Errorf("read the use grants: %w", err)
	}
	if f.UseVia, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Subject]); err != nil {
		return f, fmt.Errorf("read the use grants: %w", err)
	}
	if f.Grants, err = matchingSpaceGrants(ctx, tx, person, space); err != nil {
		return f, err
	}
	return f, readLists(ctx, tx, person, f.Chain)
}

// matchingSpaceGrants are the space's grants that name the person, a row per
// subject with its permissions in the grid's order.
func matchingSpaceGrants(ctx context.Context, tx db.DBTX, person, space uuid.UUID) ([]SpaceGrant, error) {
	rows, err := tx.Query(ctx, `SELECT `+SubjectColumns+`, g.permission FROM space_grant g`+SubjectJoins+`
		WHERE g.space_id = $2 AND perm_is_member($1) AND perm_subject_matches($1, g.subject_type, g.user_id, g.group_id)
		ORDER BY `+SubjectOrder, person, space)
	if err != nil {
		return nil, fmt.Errorf("read the space's grants: %w", err)
	}
	defer rows.Close()
	var out []SpaceGrant
	for rows.Next() {
		var (
			sub Subject
			p   SpacePermission
		)
		if err := rows.Scan(&sub.Type, &sub.ID, &sub.Name, &p); err != nil {
			return nil, err
		}
		i := slices.IndexFunc(out, func(g SpaceGrant) bool { return sameSubject(g.Subject, sub) })
		if i < 0 {
			out = append(out, SpaceGrant{Subject: sub})
			i = len(out) - 1
		}
		out[i].Permissions = append(out[i].Permissions, p)
	}
	for i := range out {
		slices.SortFunc(out[i].Permissions, func(a, b SpacePermission) int {
			return slices.Index(SpacePermissions, a) - slices.Index(SpacePermissions, b)
		})
	}
	return out, rows.Err()
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
		if kind == ListEdit {
			list = &chain[i].Edit
		}
		list.Listed = append(list.Listed, sub)
		if matches {
			list.Via = append(list.Via, sub)
		}
	}
	return rows.Err()
}
