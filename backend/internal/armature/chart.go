package armature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ChartKind is what a chart block draws.
type ChartKind string

const (
	// ChartPie shares the issues out by one field, from Armature's chart report as a donut.
	ChartPie ChartKind = "pie"
	// ChartCreatedResolved counts the issues created and resolved on each day,
	// from Armature's created_vs_resolved report.
	ChartCreatedResolved ChartKind = "createdResolved"
)

// ChartKinds are the charts a block may draw.
var ChartKinds = []ChartKind{ChartPie, ChartCreatedResolved}

// ChartGroupings are the fields a pie shares issues out by: those of
// Armature's groupBy that every project has.
var ChartGroupings = []string{"status", "statusCategory", "type", "priority", "assignee"}

const (
	// DefaultChartDays, MinChartDays and MaxChartDays bound a created against
	// resolved chart's window, within Armature's own 1 to 365.
	DefaultChartDays = 30
	MinChartDays     = 7
	MaxChartDays     = 365
	// ChartCacheTTL is as long as a search is kept, and cleared with them.
	ChartCacheTTL = SearchCacheTTL
)

// ProjectPattern is a project key as Armature writes it.
const ProjectPattern = `^[A-Z][A-Z0-9]{1,9}$`

var projectKey = regexp.MustCompile(ProjectPattern)

// ChartInput is what a chart block asks for: a project, the NQL query the
// counted issues must match, and either a field or a window of days.
type ChartInput struct {
	Project string
	Query   string
	Kind    ChartKind
	GroupBy string
	Days    int
}

// ChartSlice is one share of a pie: a field's value and how many issues
// have it. Category is the status category, for colouring, when it has one.
type ChartSlice struct {
	Label    string `json:"label"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// ChartDay is one day of a created against resolved chart.
type ChartDay struct {
	Day      string `json:"day"`
	Created  int    `json:"created"`
	Resolved int    `json:"resolved"`
}

// Chart is what a chart block draws, as the viewer may see it: Slices for a
// pie, Days for created against resolved. Total is the issues in the pie, or
// those created in the window.
type Chart struct {
	Kind    ChartKind    `json:"kind"`
	GroupBy string       `json:"groupBy"`
	Total   int          `json:"total"`
	Slices  []ChartSlice `json:"slices"`
	Days    []ChartDay   `json:"days"`
	// URL opens the query in Armature.
	URL string `json:"url"`
}

// CheckChart refuses what Armature would, before asking it.
func CheckChart(in ChartInput) error {
	if !projectKey.MatchString(in.Project) {
		return &FieldError{Field: "project", Message: "Name the project by its key, such as CP."}
	}
	if err := CheckQuery(in.Query); err != nil {
		return err
	}
	switch in.Kind {
	case ChartPie:
		if !slices.Contains(ChartGroupings, in.GroupBy) {
			return &FieldError{Field: "groupBy", Message: "Share the issues out by status, status category, type, priority or assignee."}
		}
	case ChartCreatedResolved:
		if in.Days < MinChartDays || in.Days > MaxChartDays {
			return &FieldError{Field: "days", Message: fmt.Sprintf("Count %d to %d days back.", MinChartDays, MaxChartDays)}
		}
	default:
		return &FieldError{Field: "kind", Message: "Choose a pie chart or created against resolved."}
	}
	return nil
}

// Chart asks Armature for a report as the viewer, so each reader counts the
// issues they may see. A query Armature cannot read is a 422 as in Search;
// a project the viewer may not see is a 422 that says so.
func (s *Service) Chart(ctx context.Context, in ChartInput) (Status, *Chart, error) {
	if err := CheckChart(in); err != nil {
		return "", nil, err
	}
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	var cached Chart
	if s.cache.Chart(ctx, v.OrgID, v.TokenID, in, &cached) {
		return StatusOK, &cached, nil
	}
	params := url.Values{"q": {in.Query}}
	path := "/projects/" + url.PathEscape(in.Project) + "/reports/"
	out := &Chart{Kind: in.Kind, Slices: []ChartSlice{}, Days: []ChartDay{}, URL: SearchURL(v.Caller.BaseURL(), in.Query)}
	if in.Kind == ChartPie {
		params.Set("groupBy", in.GroupBy)
		params.Set("measure", "count")
		params.Set("shape", "donut")
		var report struct {
			GroupBy string  `json:"groupBy"`
			Total   float64 `json:"total"`
			Groups  []struct {
				Category string  `json:"category"`
				Label    string  `json:"label"`
				Value    float64 `json:"value"`
			} `json:"groups"`
		}
		err = v.Caller.Get(ctx, path+"chart", params, &report)
		if err == nil {
			out.GroupBy, out.Total = in.GroupBy, int(report.Total)
			for _, g := range report.Groups {
				out.Slices = append(out.Slices, ChartSlice{Label: g.Label, Category: g.Category, Count: int(g.Value)})
			}
		}
	} else {
		params.Set("days", strconv.Itoa(in.Days))
		var report struct {
			Days []struct {
				Day      time.Time `json:"day"`
				Created  int       `json:"created"`
				Resolved int       `json:"resolved"`
			} `json:"days"`
		}
		err = v.Caller.Get(ctx, path+"created_vs_resolved", params, &report)
		if err == nil {
			for _, d := range report.Days {
				out.Days = append(out.Days, ChartDay{Day: d.Day.UTC().Format(time.DateOnly), Created: d.Created, Resolved: d.Resolved})
				out.Total += d.Created
			}
		}
	}
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
	s.cache.PutChart(ctx, v.OrgID, v.TokenID, in, out)
	return StatusOK, out, nil
}

// ChartField names one person's answer to one chart, in the search hash so
// an issue's change clears it with the searches.
func ChartField(tokenID uuid.UUID, in ChartInput) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"chart", in.Project, in.Query, string(in.Kind), in.GroupBy, strconv.Itoa(in.Days)}, "\x00")))
	return tokenID.String() + ":" + hex.EncodeToString(sum[:])
}

// Chart reads a cached chart; see Issue.
func (c *Cache) Chart(ctx context.Context, org, tokenID uuid.UUID, in ChartInput, out any) bool {
	return c.getField(ctx, SearchKey(org), ChartField(tokenID, in), ChartCacheTTL, out)
}

// PutChart stores a chart.
func (c *Cache) PutChart(ctx context.Context, org, tokenID uuid.UUID, in ChartInput, value any) {
	c.putField(ctx, SearchKey(org), ChartField(tokenID, in), ChartCacheTTL, value)
}
