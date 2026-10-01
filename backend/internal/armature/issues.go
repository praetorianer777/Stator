package armature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LookupParallel bounds the single fetches one lookup makes at once, for the
// keys its search did not return.
const LookupParallel = 8

// issueWrite is the Armature permission that files issues.
const issueWrite = "issue.write"

var keyShape = regexp.MustCompile(KeyPattern)

// NormalizeKey is raw upper case, and whether that is an issue key.
func NormalizeKey(raw string) (string, bool) {
	key := strings.ToUpper(strings.TrimSpace(raw))
	return key, keyShape.MatchString(key)
}

// IssueURL is where an issue opens in the Armature at baseURL.
func IssueURL(baseURL, key string) string {
	return strings.TrimSuffix(baseURL, "/") + "/issues/" + url.PathEscape(key)
}

// Lookup answers one result per key, in order, as the viewer may see them.
// Keys are distinct and normalized; a status other than ok has no results.
func (s *Service) Lookup(ctx context.Context, keys []string) (Status, []IssueResult, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	found := make(map[string]*Issue, len(keys))
	var missing []string
	for _, key := range keys {
		var cached *Issue
		if s.cache.Issue(ctx, v.OrgID, v.TokenID, key, &cached) {
			found[key] = cached
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) > 0 {
		fetched, err := s.fetchIssues(ctx, v, missing)
		if err != nil {
			return s.failed(ctx, v, err), nil, nil
		}
		for _, key := range missing {
			found[key] = fetched[key]
			s.cache.PutIssue(ctx, v.OrgID, v.TokenID, key, fetched[key])
		}
	}
	out := make([]IssueResult, len(keys))
	for i, key := range keys {
		out[i] = IssueResult{Key: key, Issue: found[key]}
	}
	return StatusOK, out, nil
}

// Issue is one issue as the viewer may see it, nil when they may not; the
// key is normalized.
func (s *Service) Issue(ctx context.Context, key string) (Status, *Issue, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	var issue *Issue
	if s.cache.Issue(ctx, v.OrgID, v.TokenID, key, &issue) {
		return StatusOK, issue, nil
	}
	issue, err = s.fetchIssue(ctx, v, key)
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	s.cache.PutIssue(ctx, v.OrgID, v.TokenID, key, issue)
	return StatusOK, issue, nil
}

// failed is the status a read answers for a call's error; a refused token is
// marked so it is not sent again.
func (s *Service) failed(ctx context.Context, v *Viewer, err error) Status {
	status := StatusOf(err)
	switch status {
	case StatusRejected:
		if err := s.NoteRejected(ctx, v); err != nil {
			s.opts.Log.Warn("a refused Armature token could not be marked", "token_id", v.TokenID, "error", err)
		}
	case StatusUnreachable:
		s.opts.Log.Info("Armature did not answer a read", "org_id", v.OrgID, "error", err)
	}
	return status
}

// fetchIssues is one NQL search for keys, then a fetch of each key it did not
// return, which finds an issue moved away from that key.
func (s *Service) fetchIssues(ctx context.Context, v *Viewer, keys []string) (map[string]*Issue, error) {
	out := make(map[string]*Issue, len(keys))
	var answer struct {
		Issues []Issue `json:"issues"`
	}
	query := url.Values{"q": {"key in (" + strings.Join(keys, ", ") + ")"}, "limit": {strconv.Itoa(len(keys))}}
	err := v.Caller.Get(ctx, "/issues", query, &answer)
	var refused *RefusedError
	// A search this Armature refuses still leaves every key to fetch alone.
	if err != nil && !errors.As(err, &refused) {
		return nil, err
	}
	for i := range answer.Issues {
		issue := answer.Issues[i]
		if slices.Contains(keys, issue.Key) {
			issue.URL = IssueURL(v.Caller.BaseURL(), issue.Key)
			out[issue.Key] = &issue
		}
	}
	var rest []string
	for _, key := range keys {
		if _, ok := out[key]; !ok {
			rest = append(rest, key)
		}
	}

	var (
		mu    sync.Mutex
		first error
		wg    sync.WaitGroup
		slots = make(chan struct{}, LookupParallel)
	)
	for _, key := range rest {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			issue, err := s.fetchIssue(ctx, v, key)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			out[key] = issue
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return out, nil
}

// fetchIssue is Armature's GET /issues/{key}, which answers an issue's old
// keys too; nil when there is none the viewer may see.
func (s *Service) fetchIssue(ctx context.Context, v *Viewer, key string) (*Issue, error) {
	var answer struct {
		Issue Issue `json:"issue"`
	}
	err := v.Caller.Get(ctx, "/issues/"+url.PathEscape(key), nil, &answer)
	var refused *RefusedError
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	answer.Issue.URL = IssueURL(v.Caller.BaseURL(), answer.Issue.Key)
	return &answer.Issue, nil
}

// meta is what MetaKey holds for a person.
type meta struct {
	Projects   []Project   `json:"projects"`
	IssueTypes []IssueType `json:"issueTypes"`
}

// Projects are the Armature projects the viewer may see, archived ones left
// out, each saying whether they may file issues in it.
func (s *Service) Projects(ctx context.Context) (Status, []Project, error) {
	v, status, err := s.Viewer(ctx)
	if v == nil {
		return status, nil, err
	}
	m, err := s.meta(ctx, v)
	if err != nil {
		return s.failed(ctx, v, err), nil, nil
	}
	return StatusOK, m.Projects, nil
}

// meta is the viewer's projects and issue types, cached together for
// MetaCacheTTL, so the create dialog of #31 finds both in one entry.
func (s *Service) meta(ctx context.Context, v *Viewer) (*meta, error) {
	var m meta
	if s.cache.Meta(ctx, v.OrgID, v.TokenID, &m) {
		return &m, nil
	}
	var projects struct {
		Projects []struct {
			Key        string     `json:"key"`
			Name       string     `json:"name"`
			ArchivedAt *time.Time `json:"archivedAt"`
		} `json:"projects"`
	}
	if err := v.Caller.Get(ctx, "/projects", nil, &projects); err != nil {
		return nil, err
	}
	var access struct {
		Permissions struct {
			Org      []string            `json:"org"`
			Projects map[string][]string `json:"projects"`
		} `json:"permissions"`
	}
	if err := v.Caller.Get(ctx, "/access/me", nil, &access); err != nil {
		return nil, err
	}
	var types struct {
		IssueTypes []struct {
			IssueType
			IsSubtask bool `json:"isSubtask"`
		} `json:"issueTypes"`
	}
	if err := v.Caller.Get(ctx, "/issue-types", nil, &types); err != nil {
		return nil, err
	}
	m.Projects = []Project{}
	for _, p := range projects.Projects {
		if p.ArchivedAt != nil {
			continue
		}
		m.Projects = append(m.Projects, Project{
			Key:       p.Key,
			Name:      p.Name,
			CanCreate: slices.Contains(access.Permissions.Org, issueWrite) || slices.Contains(access.Permissions.Projects[p.Key], issueWrite),
		})
	}
	m.IssueTypes = []IssueType{}
	for _, t := range types.IssueTypes {
		if !t.IsSubtask {
			m.IssueTypes = append(m.IssueTypes, t.IssueType)
		}
	}
	s.cache.PutMeta(ctx, v.OrgID, v.TokenID, m)
	return &m, nil
}
