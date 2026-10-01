package armature

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const (
	// RestrictedLinkTitle names a page on an issue when not every member may
	// view it, since its own title could say more than its readers want told.
	RestrictedLinkTitle = "A restricted page in Stator"
	// MaxLinkTitle is Armature's bound on a remote link's title.
	MaxLinkTitle = 255
)

// KeysIn lists the distinct issue keys a document names as chips and issue
// blocks, in the order first named. List blocks are queries, not mentions.
func KeysIn(body json.RawMessage) []string {
	var doc any
	if len(body) == 0 || json.Unmarshal(body, &doc) != nil {
		return nil
	}
	var keys []string
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case []any:
			for _, child := range v {
				walk(child)
			}
		case map[string]any:
			if kind, _ := v["type"].(string); kind == NodeIssue || kind == NodeIssueBlock {
				if attrs, ok := v["attrs"].(map[string]any); ok {
					raw, _ := attrs["key"].(string)
					if key, ok := NormalizeKey(raw); ok && !slices.Contains(keys, key) {
						keys = append(keys, key)
					}
				}
			}
			walk(v["content"])
		}
	}
	walk(doc)
	return keys
}

// LinkTitle is how a page is titled on the issues it names.
func LinkTitle(title string, open bool) string {
	if !open {
		return RestrictedLinkTitle
	}
	if utf8.RuneCountInString(title) > MaxLinkTitle {
		return string([]rune(title)[:MaxLinkTitle])
	}
	return title
}

// WantedLink is the remote link a page should have on one issue.
type WantedLink struct {
	URL   string
	Title string
}

// LinkRecord is what the last sync did for one issue a page named.
type LinkRecord struct {
	Key      string
	RemoteID *uuid.UUID
	URL      string
	Title    string
	State    LinkState
}

// LinkStep is one change to make in Armature: Want set puts the link, nil
// takes Have's off the issue.
type LinkStep struct {
	Key  string
	Want *WantedLink
	Have *LinkRecord
}

// PlanLinks compares what a page should have in Armature with what the last
// sync recorded, by key. A link synced as wanted is left alone; a failed or
// pending one is tried again.
func PlanLinks(want map[string]WantedLink, have []LinkRecord) []LinkStep {
	held := make(map[string]*LinkRecord, len(have))
	for i := range have {
		held[have[i].Key] = &have[i]
	}
	var steps []LinkStep
	for key, w := range want {
		rec := held[key]
		if rec != nil && rec.State == LinkSynced && rec.RemoteID != nil && rec.URL == w.URL && rec.Title == w.Title {
			continue
		}
		steps = append(steps, LinkStep{Key: key, Want: &WantedLink{URL: w.URL, Title: w.Title}, Have: rec})
	}
	for key, rec := range held {
		if _, ok := want[key]; !ok {
			steps = append(steps, LinkStep{Key: key, Have: rec})
		}
	}
	slices.SortFunc(steps, func(a, b LinkStep) int { return strings.Compare(a.Key, b.Key) })
	return steps
}

// RemoteLinkRequest is what link sync sends Armature to put a page on an issue.
type RemoteLinkRequest struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Source string `json:"source"`
}

// LinkSync is the worker's handler for armature.links: it brings one page's
// remote links in Armature in line with what the page names now.
type LinkSync struct {
	svc *Service
	log *slog.Logger
}

// NewLinkSync builds the handler on the service whose AppURL the links name.
func NewLinkSync(svc *Service, log *slog.Logger) *LinkSync {
	return &LinkSync{svc: svc, log: log}
}

// pageState is what a sync needs to know of a page; Found is false once it is
// purged.
type pageState struct {
	Found     bool
	Title     string
	SpaceKey  string
	Body      json.RawMessage
	Published bool
	Trashed   bool
	Open      bool
}

// Handle syncs the page the event names, as the event's actor with their own
// token. Run twice, the second run finds every link recorded and sends nothing.
func (l *LinkSync) Handle(ctx context.Context, e events.Event) error {
	var in events.ArmatureLinks
	if err := json.Unmarshal(e.Payload, &in); err != nil || in.PageID == uuid.Nil || e.OrgID == uuid.Nil {
		return nil
	}
	org := tenant.WithOrg(db.PinPrimary(ctx), tenant.Org{ID: e.OrgID})
	var retry error
	_, err := l.svc.db.WriteAdmin(org, func(ctx context.Context, tx db.DBTX) error {
		// Two workers syncing one page at once would each put the links the
		// other is about to record.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('armature-links:' || $1::text, 0))`, in.PageID); err != nil {
			return err
		}
		connected, page, have, err := l.read(ctx, tx, in.PageID)
		if err != nil || !connected {
			return err
		}
		want := map[string]WantedLink{}
		if page.Found && page.Published && !page.Trashed {
			link := WantedLink{URL: PageURL(l.svc.opts.AppURL, page.SpaceKey, in.PageID), Title: LinkTitle(page.Title, page.Open)}
			for _, key := range KeysIn(page.Body) {
				want[key] = link
			}
		}
		steps := PlanLinks(want, have)
		if len(steps) == 0 {
			return nil
		}
		run := &linkRun{sync: l, tx: tx, pageID: in.PageID, as: db.WithUser(org, in.ActorID)}
		if run.viewer, run.status, err = l.svc.Viewer(run.as); err != nil {
			return err
		}
		if err := run.pending(ctx, steps); err != nil {
			return err
		}
		if run.name, err = actorName(ctx, tx, in.ActorID); err != nil {
			return err
		}
		for _, step := range steps {
			if retry, err = run.apply(ctx, step); err != nil || retry != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return retry
}

func (l *LinkSync) read(ctx context.Context, tx db.DBTX, pageID uuid.UUID) (bool, pageState, []LinkRecord, error) {
	var page pageState
	var connected bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM armature_connection WHERE org_id = current_org_id())`).Scan(&connected); err != nil || !connected {
		return false, page, nil, err
	}
	err := tx.QueryRow(ctx, `
		SELECT p.title, s.key, p.body, p.version > 0, p.trashed_at IS NOT NULL, page_open_to_members(p.id)
		FROM page p JOIN space s ON s.id = p.space_id
		WHERE p.id = $1 AND p.org_id = current_org_id()`, pageID).
		Scan(&page.Title, &page.SpaceKey, &page.Body, &page.Published, &page.Trashed, &page.Open)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return false, page, nil, fmt.Errorf("read the page: %w", err)
	default:
		page.Found = true
	}
	rows, err := tx.Query(ctx, `
		SELECT issue_key, remote_link_id, COALESCE(url, ''), COALESCE(title, ''), state
		FROM armature_remote_link WHERE org_id = current_org_id() AND page_id = $1`, pageID)
	if err != nil {
		return false, page, nil, err
	}
	have, err := pgx.CollectRows(rows, pgx.RowToStructByPos[LinkRecord])
	return true, page, have, err
}

func actorName(ctx context.Context, tx db.DBTX, id uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT NULLIF(name, '') FROM app_user WHERE id = $1), '')`, id).Scan(&name)
	return name, err
}

// linkRun is one event's sync, as one person.
type linkRun struct {
	sync   *LinkSync
	tx     db.DBTX
	pageID uuid.UUID
	// as is the actor's own context, for what the service writes as them.
	as     context.Context
	viewer *Viewer
	status Status
	name   string
}

// apply makes one step in Armature and records it. A retry is an Armature
// that did not answer: the event fails and the outbox tries it again.
func (r *linkRun) apply(ctx context.Context, step LinkStep) (retry error, err error) {
	if step.Want == nil && step.Have.RemoteID == nil {
		return nil, r.forget(ctx, step.Key)
	}
	if r.viewer == nil {
		return nil, r.fail(ctx, step, r.withoutToken())
	}
	old := step.Have
	if old != nil && old.RemoteID != nil && (step.Want == nil || old.URL != step.Want.URL) {
		if why, retry := r.call(ctx, http.MethodDelete, linkPath(step.Key)+"/"+old.RemoteID.String(), nil, nil); retry != nil || why != "" {
			if retry != nil {
				return retry, nil
			}
			return nil, r.fail(ctx, step, why)
		}
		if step.Want == nil {
			return nil, r.forget(ctx, step.Key)
		}
		if _, err := r.tx.Exec(ctx, `
			UPDATE armature_remote_link SET remote_link_id = NULL, updated_at = now()
			WHERE org_id = current_org_id() AND page_id = $1 AND issue_key = $2`, r.pageID, step.Key); err != nil {
			return nil, err
		}
	}
	var answer struct {
		RemoteLink struct {
			ID uuid.UUID `json:"id"`
		} `json:"remoteLink"`
	}
	body := RemoteLinkRequest{URL: step.Want.URL, Title: step.Want.Title, Source: LinkSource}
	why, retry := r.call(ctx, http.MethodPost, linkPath(step.Key), body, &answer)
	switch {
	case retry != nil:
		return retry, nil
	case why != "":
		return nil, r.fail(ctx, step, why)
	}
	_, err = r.tx.Exec(ctx, `
		INSERT INTO armature_remote_link (org_id, page_id, issue_key, remote_link_id, url, title, state, error, synced_at, updated_at)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, 'synced', NULL, now(), now())
		ON CONFLICT (org_id, page_id, issue_key) DO UPDATE SET
		    remote_link_id = EXCLUDED.remote_link_id, url = EXCLUDED.url, title = EXCLUDED.title,
		    state = 'synced', error = NULL, synced_at = now(), updated_at = now()`,
		r.pageID, step.Key, answer.RemoteLink.ID, step.Want.URL, step.Want.Title)
	return nil, err
}

// call sends one request as the run's person. why is the sentence for a
// refusal another try would meet again; retry is an Armature that did not
// answer. A 404 on a delete means the link is gone already.
func (r *linkRun) call(ctx context.Context, method, path string, body, out any) (why string, retry error) {
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	err := r.viewer.Caller.Send(callCtx, method, path, body, out)
	var refused *RefusedError
	switch {
	case err == nil:
		return "", nil
	case errors.Is(err, ErrRejected):
		if nerr := r.sync.svc.NoteRejected(r.as, r.viewer); nerr != nil {
			r.sync.log.Warn("a rejected Armature token could not be marked", "error", nerr)
		}
		r.viewer, r.status = nil, StatusRejected
		return r.withoutToken(), nil
	case errors.As(err, &refused) && refused.Status == http.StatusNotFound && method == http.MethodDelete:
		return "", nil
	case errors.As(err, &refused):
		return r.refusal(refused), nil
	default:
		r.sync.log.Info("Armature did not answer a link sync; the outbox tries again", "page_id", r.pageID, "error", err)
		return "", err
	}
}

func (r *linkRun) refusal(e *RefusedError) string {
	again := " The next change to the page tries again."
	switch {
	case e.Code == "read_only_token":
		return r.person() + "'s Armature token may only read. Store one that may write under Profile, Armature." + again
	case e.Status == http.StatusForbidden:
		return "Armature did not let " + r.person() + " link pages to this issue. Somebody who may edit the issue in Armature can change the page." + again
	case e.Status == http.StatusNotFound:
		return "Armature has no issue with this key that " + r.person() + " may see. Check the key in the page." + again
	}
	return "Armature refused the link: " + strings.TrimSpace(e.Message) + again
}

func (r *linkRun) withoutToken() string {
	again := " The next change to the page tries again."
	switch r.status {
	case StatusRejected:
		return r.person() + "'s Armature token was not accepted. Store a new one under Profile, Armature." + again
	default:
		return r.person() + " has not connected an Armature account, so the link was not made. Connect one under Profile, Armature." + again
	}
}

// person names the actor in a sentence that begins with it.
func (r *linkRun) person() string {
	if r.name == "" {
		return "The person who changed the page"
	}
	return r.name
}

// pending marks every link about to be put as not there yet, which it stays
// while Armature does not answer.
func (r *linkRun) pending(ctx context.Context, steps []LinkStep) error {
	var keys []string
	for _, step := range steps {
		if step.Want != nil {
			keys = append(keys, step.Key)
		}
	}
	_, err := r.tx.Exec(ctx, `
		INSERT INTO armature_remote_link (org_id, page_id, issue_key, state, updated_at)
		SELECT current_org_id(), $1, k, 'pending', now() FROM unnest($2::text[]) AS k
		ON CONFLICT (org_id, page_id, issue_key) DO UPDATE SET state = 'pending', error = NULL, updated_at = now()`,
		r.pageID, keys)
	return err
}

// fail marks a key failed with why, keeping what was recorded of its link so
// a later sync can still take it off.
func (r *linkRun) fail(ctx context.Context, step LinkStep, why string) error {
	_, err := r.tx.Exec(ctx, `
		INSERT INTO armature_remote_link (org_id, page_id, issue_key, state, error, updated_at)
		VALUES (current_org_id(), $1, $2, 'failed', $3, now())
		ON CONFLICT (org_id, page_id, issue_key) DO UPDATE SET state = 'failed', error = EXCLUDED.error, updated_at = now()`,
		r.pageID, step.Key, why)
	return err
}

func (r *linkRun) forget(ctx context.Context, key string) error {
	_, err := r.tx.Exec(ctx, `DELETE FROM armature_remote_link WHERE org_id = current_org_id() AND page_id = $1 AND issue_key = $2`, r.pageID, key)
	return err
}

func linkPath(key string) string {
	return "/issues/" + url.PathEscape(key) + "/remote-links"
}

// PageLinks is how the remote link of each issue a page's published body
// names stands, read as the caller; a key the worker has not reached yet is
// pending. Without a connection nothing is synced, so nothing is listed.
func (s *Service) PageLinks(ctx context.Context, pageID uuid.UUID, body json.RawMessage) ([]Link, error) {
	out := []Link{}
	keys := KeysIn(body)
	if len(keys) == 0 {
		return out, nil
	}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := s.endpoint(ctx, tx)
		if err != nil || e == nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT issue_key, state, error, synced_at FROM armature_remote_link
			WHERE org_id = current_org_id() AND page_id = $1`, pageID)
		if err != nil {
			return err
		}
		held, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Link, error) {
			var l Link
			err := row.Scan(&l.Key, &l.State, &l.Error, &l.SyncedAt)
			return l, err
		})
		if err != nil {
			return err
		}
		for _, key := range keys {
			link := Link{Key: key, State: LinkPending}
			if i := slices.IndexFunc(held, func(l Link) bool { return l.Key == key }); i >= 0 {
				link = held[i]
			}
			out = append(out, link)
		}
		return nil
	})
	return out, err
}
