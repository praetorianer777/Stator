package armature

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CalendarCacheTTL is as long as a search is kept, and cleared with them.
const CalendarCacheTTL = SearchCacheTTL

// MonthPattern is a month as Armature's calendar takes it.
const MonthPattern = `^[0-9]{4}-(0[1-9]|1[0-2])$`

var monthShape = regexp.MustCompile(MonthPattern)

// calendarIssueKind is the kind of Armature's calendar items that are
// issues; its sprints, milestones and versions are not a team's dates.
const calendarIssueKind = "issue"

// CalendarIssue is an issue Armature dates in a month: From is its first
// day, To its due day, each YYYY-MM-DD.
type CalendarIssue struct {
	Key     string `json:"key"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
	From    string `json:"from"`
	To      string `json:"to"`
	Done    bool   `json:"done"`
	// Category is the issue's status category, for its colour.
	Category string `json:"category"`
}

// CalendarMonth is a project's dated issues in one month, as the viewer may
// see them. Truncated says Armature left some out.
type CalendarMonth struct {
	Month     string          `json:"month"`
	Issues    []CalendarIssue `json:"issues"`
	Truncated bool            `json:"truncated"`
}

// CheckCalendar refuses what Armature would, before asking it.
func CheckCalendar(project, month string) error {
	if !projectKey.MatchString(project) {
		return &FieldError{Field: "project", Message: "Name the project by its key, such as CP."}
	}
	if !monthShape.MatchString(month) {
		return &FieldError{Field: "month", Message: "Name the month as YYYY-MM, such as 2026-10."}
	}
	return nil
}

// calendarItem is one thing Armature's calendar dates in a month.
type calendarItem struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	From     string `json:"from"`
	To       string `json:"to"`
	Done     bool   `json:"done"`
	Category string `json:"category"`
}

// Calendar asks Armature for a project's dated issues in a month as the
// viewer, so each reader sees the issues they may. Refusals are as in Roadmap.
func (s *Service) Calendar(ctx context.Context, project, month string) (Status, *CalendarMonth, error) {
	if err := CheckCalendar(project, month); err != nil {
		return "", nil, err
	}
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	var cached CalendarMonth
	if s.cache.Calendar(ctx, v.OrgID, v.TokenID, project, month, &cached) {
		return StatusOK, &cached, nil
	}
	var answer struct {
		Month struct {
			Items     []calendarItem `json:"items"`
			Truncated bool           `json:"truncated"`
		} `json:"month"`
	}
	err = v.Caller.Get(ctx, "/projects/"+url.PathEscape(project)+"/calendar", url.Values{"month": {month}}, &answer)
	var refused *RefusedError
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden) {
		return "", nil, &FieldError{Field: "project", Message: "That project was not found in Armature, or you may not see it. Check its key."}
	}
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	out := buildCalendar(month, answer.Month.Items, v.Caller.BaseURL())
	out.Truncated = answer.Month.Truncated
	s.cache.PutCalendar(ctx, v.OrgID, v.TokenID, project, month, out)
	return StatusOK, out, nil
}

// buildCalendar keeps the issues of a month's items, by their first day.
func buildCalendar(month string, items []calendarItem, baseURL string) *CalendarMonth {
	out := &CalendarMonth{Month: month, Issues: []CalendarIssue{}}
	for _, it := range items {
		from, okFrom := dayOfText(it.From)
		to, okTo := dayOfText(it.To)
		if it.Kind != calendarIssueKind || it.Key == "" || !okFrom || !okTo {
			continue
		}
		if to < from {
			from, to = to, from
		}
		out.Issues = append(out.Issues, CalendarIssue{
			Key: it.Key, URL: IssueURL(baseURL, it.Key), Summary: it.Title, From: from, To: to, Done: it.Done, Category: it.Category,
		})
	}
	slices.SortStableFunc(out.Issues, func(a, b CalendarIssue) int {
		return cmp.Or(strings.Compare(a.From, b.From), compareKeys(a.Key, b.Key))
	})
	return out
}

// dayOfText reads the day of a date or a time Armature writes.
func dayOfText(s string) (string, bool) {
	if len(s) < len(time.DateOnly) {
		return "", false
	}
	d := s[:len(time.DateOnly)]
	_, err := time.Parse(time.DateOnly, d)
	return d, err == nil
}

// CalendarField names one person's answer to one month of a project, in the
// search hash so an issue's change clears it with the searches.
func CalendarField(tokenID uuid.UUID, project, month string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"calendar", project, month}, "\x00")))
	return tokenID.String() + ":" + hex.EncodeToString(sum[:])
}

// Calendar reads a cached month; see Issue.
func (c *Cache) Calendar(ctx context.Context, org, tokenID uuid.UUID, project, month string, out any) bool {
	return c.getField(ctx, SearchKey(org), CalendarField(tokenID, project, month), CalendarCacheTTL, out)
}

// PutCalendar stores a month.
func (c *Cache) PutCalendar(ctx context.Context, org, tokenID uuid.UUID, project, month string, value any) {
	c.putField(ctx, SearchKey(org), CalendarField(tokenID, project, month), CalendarCacheTTL, value)
}
