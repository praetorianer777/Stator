package exciter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

var keyShape = regexp.MustCompile(KeyPattern)

// NormalizeKey is raw upper case, and whether that is a requirement key.
func NormalizeKey(raw string) (string, bool) {
	key := strings.ToUpper(strings.TrimSpace(raw))
	return key, len(key) <= MaxKeyLength && keyShape.MatchString(key)
}

// RequirementURL is where a requirement opens in the Exciter at baseURL.
func RequirementURL(baseURL, projectKey, key string) string {
	return strings.TrimSuffix(baseURL, "/") + "/p/" + url.PathEscape(projectKey) + "/r/" + url.PathEscape(key)
}

// requirementKey is the hash holding one key's answers, one field per token row.
func (s *Service) requirementKey(site uuid.UUID, key string) string {
	return s.cache.Key(site, "requirement:"+key)
}

// idKey is the set of the keys a requirement was read by, so a webhook that
// names it by id and its current key clears an answer read by a former key.
func (s *Service) idKey(site, id uuid.UUID) string {
	return s.cache.Key(site, "requirement-id:"+id.String())
}

// cached reads the viewer's answer for key; false is a miss.
func (s *Service) cached(ctx context.Context, v *Viewer, key string) (*RequirementDetail, bool) {
	var r *RequirementDetail
	if s.cache.Field(ctx, s.requirementKey(v.SiteID, key), v.TokenID.String(), RequirementCacheTTL, &r) {
		return r, true
	}
	return nil, false
}

func (s *Service) keep(ctx context.Context, v *Viewer, key string, r *RequirementDetail) {
	s.cache.PutField(ctx, s.requirementKey(v.SiteID, key), v.TokenID.String(), RequirementCacheTTL, r)
	if r != nil {
		s.cache.Remember(ctx, s.idKey(v.SiteID, r.ID), key, RequirementCacheTTL)
	}
}

// Lookup answers one result per key, in order, as the viewer may see them.
// Keys are distinct and normalized; a status other than ok has no results.
func (s *Service) Lookup(ctx context.Context, keys []string) (Status, []RequirementResult, error) {
	status, found, err := s.details(ctx, keys)
	if status != StatusOK || err != nil {
		return status, nil, err
	}
	out := make([]RequirementResult, len(keys))
	for i, key := range keys {
		out[i] = RequirementResult{Key: key}
		if d := found[key]; d != nil {
			out[i].Requirement = &d.Requirement
		}
	}
	return StatusOK, out, nil
}

// Requirement is one requirement with its statement and rationale, as the
// viewer may see it, nil when they may not; the key is normalized.
func (s *Service) Requirement(ctx context.Context, key string) (Status, *RequirementDetail, error) {
	status, found, err := s.details(ctx, []string{key})
	return status, found[key], err
}

// details reads each key as the viewer, from the cache where it can; a chip
// and a block of the same key share the one answer.
func (s *Service) details(ctx context.Context, keys []string) (Status, map[string]*RequirementDetail, error) {
	v, status, err := s.ViewerFor(ctx, FeatureRequirements)
	if v == nil {
		return status, nil, err
	}
	found := make(map[string]*RequirementDetail, len(keys))
	var missing []string
	for _, key := range keys {
		if r, ok := s.cached(ctx, v, key); ok {
			found[key] = r
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) > 0 {
		fetched, err := s.fetchAll(ctx, v, missing)
		if err != nil {
			return s.failed(ctx, v, err), nil, nil
		}
		for _, key := range missing {
			found[key] = fetched[key]
			s.keep(ctx, v, key, fetched[key])
		}
	}
	return StatusOK, found, nil
}

// failed is the status a read answers for a call's error; a refused token is
// marked so it is not sent again.
func (s *Service) failed(ctx context.Context, v *Viewer, err error) Status {
	status := armature.StatusOf(err)
	switch status {
	case StatusRejected:
		if err := s.noteRejected(ctx, v); err != nil {
			s.opts.Log.Warn("a refused Exciter token could not be marked", "token_id", v.TokenID, "error", err)
		}
	case StatusUnreachable:
		s.opts.Log.Info("Exciter did not answer a read", "site_id", v.SiteID, "error", err)
	}
	return status
}

// fetchAll reads each key on its own, a few at a time: Exciter answers a
// former key by the requirement it names now, which no search would.
func (s *Service) fetchAll(ctx context.Context, v *Viewer, keys []string) (map[string]*RequirementDetail, error) {
	var (
		mu    sync.Mutex
		first error
		wg    sync.WaitGroup
		slots = make(chan struct{}, LookupParallel)
		out   = make(map[string]*RequirementDetail, len(keys))
	)
	for _, key := range keys {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			r, err := s.fetch(ctx, v, key)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			out[key] = r
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return out, nil
}

// fetched is the part of Exciter's Requirement a chip and a block read.
type fetched struct {
	ID         uuid.UUID          `json:"id"`
	Key        string             `json:"key"`
	ProjectKey string             `json:"projectKey"`
	Title      string             `json:"title"`
	Revision   int                `json:"revision"`
	Status     *RequirementStatus `json:"status"`
	TrashedAt  *time.Time         `json:"trashedAt"`
	Statement  json.RawMessage    `json:"statement"`
	Rationale  json.RawMessage    `json:"rationale"`
}

// fetch is Exciter's GET /requirements/{reqKey}, which answers a former key
// too; nil when there is none the viewer may see, which Exciter says with 404.
func (s *Service) fetch(ctx context.Context, v *Viewer, key string) (*RequirementDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, armature.CallTimeout)
	defer cancel()
	var answer struct {
		Requirement fetched `json:"requirement"`
	}
	err := v.Caller.Get(ctx, "/requirements/"+url.PathEscape(key), nil, &answer)
	var refused *armature.RefusedError
	if errors.As(err, &refused) && (refused.Status == http.StatusNotFound || refused.Status == http.StatusForbidden) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f := answer.Requirement
	return &RequirementDetail{
		Requirement: Requirement{
			ID: f.ID, Key: f.Key, URL: RequirementURL(v.Caller.BaseURL(), f.ProjectKey, f.Key), ProjectKey: f.ProjectKey,
			Title: f.Title, Status: f.Status, Revision: f.Revision, TrashedAt: f.TrashedAt,
		},
		Statement: documentOrNull(f.Statement), Rationale: documentOrNull(f.Rationale),
	}, nil
}

// documentOrNull is a document as Exciter answered it, or null for none, so
// a block never has to tell an absent text from a missing field.
func documentOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}
