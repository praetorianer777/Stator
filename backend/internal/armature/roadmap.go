package armature

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RoadmapGrouping is what a roadmap block puts its rows under.
type RoadmapGrouping string

const (
	// RoadmapByEpic puts each issue under the epic above it.
	RoadmapByEpic RoadmapGrouping = "epic"
	// RoadmapByTeam puts each issue under its team.
	RoadmapByTeam RoadmapGrouping = "team"
)

// RoadmapGroupings are the groupings a block may choose.
var RoadmapGroupings = []RoadmapGrouping{RoadmapByEpic, RoadmapByTeam}

const (
	// MaxRoadmapRows is as many issues as a block draws; past it, Hidden counts the rest.
	MaxRoadmapRows = 100
	// EpicLevel is the issue type level of an epic in Armature; an initiative
	// above it is drawn as a row, not a group.
	EpicLevel = 1
	// RoadmapCacheTTL is as long as a search is kept, and cleared with them.
	RoadmapCacheTTL = SearchCacheTTL
)

// RoadmapInput is what a roadmap block asks for: a project, the NQL query its
// issues must match, and what to group them by.
type RoadmapInput struct {
	Project string
	Query   string
	GroupBy RoadmapGrouping
}

// RoadmapBar is one issue on the timeline. Start and Due are days, either may
// be null; Derived says Armature took them from the issue's children.
type RoadmapBar struct {
	Key     string      `json:"key"`
	URL     string      `json:"url"`
	Summary string      `json:"summary"`
	Type    IssueType   `json:"type"`
	Status  IssueStatus `json:"status"`
	Start   *string     `json:"start"`
	Due     *string     `json:"due"`
	Derived bool        `json:"derived"`
}

// RoadmapGroup is one epic's or one team's rows. Name is empty for the
// issues without one; Epic is the epic's own bar when grouped by epic.
type RoadmapGroup struct {
	Name string       `json:"name"`
	Epic *RoadmapBar  `json:"epic"`
	Rows []RoadmapBar `json:"rows"`
}

// Roadmap is what a roadmap block draws, as the viewer may see it. From and
// To span every day drawn, null when there is none. Unscheduled counts the
// matched issues with neither day, which are not drawn, epics with no rows too.
type Roadmap struct {
	GroupBy     RoadmapGrouping `json:"groupBy"`
	From        *string         `json:"from"`
	To          *string         `json:"to"`
	Groups      []RoadmapGroup  `json:"groups"`
	Unscheduled int             `json:"unscheduled"`
	Hidden      int             `json:"hidden"`
	// URL opens the query in Armature.
	URL string `json:"url"`
}

// CheckRoadmap refuses what Armature would, before asking it.
func CheckRoadmap(in RoadmapInput) error {
	if !projectKey.MatchString(in.Project) {
		return &FieldError{Field: "project", Message: "Name the project by its key, such as CP."}
	}
	if err := CheckQuery(in.Query); err != nil {
		return err
	}
	if !slices.Contains(RoadmapGroupings, in.GroupBy) {
		return &FieldError{Field: "groupBy", Message: "Group the issues by epic or by team."}
	}
	return nil
}

// planItem is one issue of Armature's plan, with the days it is drawn over.
type planItem struct {
	Issue struct {
		Issue
		Team *struct {
			Name string `json:"name"`
		} `json:"team"`
	} `json:"issue"`
	Start    *time.Time `json:"start"`
	Due      *time.Time `json:"due"`
	Derived  bool       `json:"derived"`
	Children []planItem `json:"children"`
}

// Roadmap asks Armature for the project's plan as the viewer, so each reader
// sees the issues they may. Refusals are as in Chart.
func (s *Service) Roadmap(ctx context.Context, in RoadmapInput) (Status, *Roadmap, error) {
	if err := CheckRoadmap(in); err != nil {
		return "", nil, err
	}
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	var cached Roadmap
	if s.cache.Roadmap(ctx, v.OrgID, v.TokenID, in, &cached) {
		return StatusOK, &cached, nil
	}
	var plan struct {
		Items   []planItem `json:"items"`
		Matched []string   `json:"matched"`
	}
	err = v.Caller.Get(ctx, "/projects/"+url.PathEscape(in.Project)+"/plan", url.Values{"q": {in.Query}}, &plan)
	var refused *RefusedError
	if errors.As(err, &refused) {
		switch {
		case refused.Code == BadQueryCode:
			return "", nil, &RefusedError{Status: http.StatusUnprocessableEntity, Code: BadQueryCode, Message: refused.Message, Position: refused.Position}
		case refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden:
			return "", nil, &FieldError{Field: "project", Message: "That project was not found in Armature, or you may not see it. Check its key."}
		}
	}
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	out := buildRoadmap(plan.Items, plan.Matched, in.GroupBy, v.Caller.BaseURL())
	out.URL = SearchURL(v.Caller.BaseURL(), in.Query)
	s.cache.PutRoadmap(ctx, v.OrgID, v.TokenID, in, out)
	return StatusOK, out, nil
}

// buildRoadmap groups the matched issues of a plan. Armature returns the
// plan whole, so an epic heads its group even when the query leaves it out.
func buildRoadmap(items []planItem, matched []string, by RoadmapGrouping, baseURL string) *Roadmap {
	wanted := map[string]bool{}
	for _, key := range matched {
		wanted[key] = true
	}
	out := &Roadmap{GroupBy: by, Groups: []RoadmapGroup{}}
	groups := map[string]*RoadmapGroup{}
	var order []*RoadmapGroup
	// headed are the groups whose epic the query matched itself.
	headed := map[*RoadmapGroup]bool{}
	groupFor := func(id, name string, epic *RoadmapBar) *RoadmapGroup {
		g, ok := groups[id]
		if !ok {
			g = &RoadmapGroup{Name: name, Epic: epic, Rows: []RoadmapBar{}}
			groups[id] = g
			order = append(order, g)
		}
		return g
	}
	var walk func(list []planItem, epic *planItem)
	walk = func(list []planItem, epic *planItem) {
		for i := range list {
			it := &list[i]
			below := epic
			if it.Issue.Type.Level == EpicLevel {
				below = it
			}
			if wanted[it.Issue.Key] {
				bar := barOf(it, baseURL)
				switch {
				case by == RoadmapByEpic && below != nil:
					// A matched epic heads its group, rows or none.
					head := barOf(below, baseURL)
					g := groupFor("epic:"+below.Issue.Key, below.Issue.Summary, &head)
					if below == it {
						headed[g] = true
					} else {
						g.Rows = append(g.Rows, bar)
					}
				case by == RoadmapByTeam && it.Issue.Team != nil:
					g := groupFor("team:"+it.Issue.Team.Name, it.Issue.Team.Name, nil)
					g.Rows = append(g.Rows, bar)
				default:
					g := groupFor("none", "", nil)
					g.Rows = append(g.Rows, bar)
				}
			}
			walk(it.Children, below)
		}
	}
	walk(items, nil)

	byStart := func(a, b RoadmapBar) int {
		return cmp.Or(compareDays(first(a.Start, a.Due), first(b.Start, b.Due)), compareKeys(a.Key, b.Key))
	}
	slices.SortStableFunc(order, func(a, b *RoadmapGroup) int {
		// The issues without an epic or a team come last.
		if (a.Name == "") != (b.Name == "") {
			return cmp.Compare(boolRank(a.Name == ""), boolRank(b.Name == ""))
		}
		if a.Epic != nil && b.Epic != nil {
			return byStart(*a.Epic, *b.Epic)
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	left := MaxRoadmapRows
	for _, g := range order {
		g.Rows = slices.DeleteFunc(g.Rows, func(bar RoadmapBar) bool {
			if bar.Start == nil && bar.Due == nil {
				out.Unscheduled++
				return true
			}
			return false
		})
		slices.SortStableFunc(g.Rows, byStart)
		if len(g.Rows) > left {
			out.Hidden += len(g.Rows) - left
			g.Rows = g.Rows[:left]
		}
		left -= len(g.Rows)
		drawn := g.Rows
		if g.Epic != nil && (g.Epic.Start != nil || g.Epic.Due != nil) {
			drawn = append([]RoadmapBar{*g.Epic}, drawn...)
		}
		if len(drawn) == 0 {
			if headed[g] {
				out.Unscheduled++
			}
			continue
		}
		out.Groups = append(out.Groups, *g)
		for _, bar := range drawn {
			for _, d := range []*string{bar.Start, bar.Due} {
				if d != nil && (out.From == nil || *d < *out.From) {
					out.From = d
				}
				if d != nil && (out.To == nil || *d > *out.To) {
					out.To = d
				}
			}
		}
	}
	return out
}

func barOf(it *planItem, baseURL string) RoadmapBar {
	return RoadmapBar{
		Key: it.Issue.Key, URL: IssueURL(baseURL, it.Issue.Key), Summary: it.Issue.Summary,
		Type: it.Issue.Type, Status: it.Issue.Status, Start: dayOf(it.Start), Due: dayOf(it.Due), Derived: it.Derived,
	}
}

func dayOf(t *time.Time) *string {
	if t == nil {
		return nil
	}
	d := t.UTC().Format(time.DateOnly)
	return &d
}

func first(days ...*string) *string {
	for _, d := range days {
		if d != nil {
			return d
		}
	}
	return nil
}

// compareDays puts a missing day last.
func compareDays(a, b *string) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return strings.Compare(*a, *b)
}

// compareKeys orders CP-9 before CP-10.
func compareKeys(a, b string) int {
	pa, na, _ := strings.Cut(a, "-")
	pb, nb, _ := strings.Cut(b, "-")
	return cmp.Or(strings.Compare(pa, pb), cmp.Compare(len(na), len(nb)), strings.Compare(na, nb))
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// RoadmapField names one person's answer to one roadmap, in the search hash
// so an issue's change clears it with the searches.
func RoadmapField(tokenID uuid.UUID, in RoadmapInput) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"roadmap", in.Project, in.Query, string(in.GroupBy)}, "\x00")))
	return tokenID.String() + ":" + hex.EncodeToString(sum[:])
}

// Roadmap reads a cached roadmap; see Issue.
func (c *Cache) Roadmap(ctx context.Context, org, tokenID uuid.UUID, in RoadmapInput, out any) bool {
	return c.getField(ctx, SearchKey(org), RoadmapField(tokenID, in), RoadmapCacheTTL, out)
}

// PutRoadmap stores a roadmap.
func (c *Cache) PutRoadmap(ctx context.Context, org, tokenID uuid.UUID, in RoadmapInput, value any) {
	c.putField(ctx, SearchKey(org), RoadmapField(tokenID, in), RoadmapCacheTTL, value)
}
