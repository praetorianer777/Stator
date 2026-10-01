//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/comment"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/freshness"
	"github.com/praetorianer777/stator/backend/internal/home"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/notify"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/reaction"
	"github.com/praetorianer777/stator/backend/internal/search"
	"github.com/praetorianer777/stator/backend/internal/space"
	"github.com/praetorianer777/stator/backend/internal/stale"
	"github.com/praetorianer777/stator/backend/internal/star"
	"github.com/praetorianer777/stator/backend/internal/tenant"
	"github.com/praetorianer777/stator/backend/internal/theme"
	"github.com/praetorianer777/stator/backend/internal/watch"
)

// apiServer is the real router over the real database and the real bucket,
// signing callers in with real sessions.
type apiServer struct {
	srv      *httptest.Server
	accounts *auth.Service
	store    objectstore.Store
	themes   *theme.Service
	// attachments takes files up to testUploadLimit, so a refusal is cheap.
	attachments *attachment.Service
	h           *harness

	mu         sync.Mutex
	lastWriter *client
}

// handOver waits for the replica when somebody reads after another person
// wrote: only the writer's own reads are promised the write at once.
func (a *apiServer) handOver(t *testing.T, c *client, method string) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lastWriter != nil && a.lastWriter != c && !c.eager {
		a.h.settle(t)
		a.lastWriter = nil
	}
	if method != http.MethodGet && method != http.MethodHead {
		a.lastWriter = c
	}
}

func newAPIServer(t *testing.T, h *harness) *apiServer {
	t.Helper()
	if os.Getenv("STATOR_S3_ENDPOINT") == "" {
		t.Fatal("STATOR_S3_ENDPOINT is not set; run the suite with make test-integration against the running stack")
	}
	store, err := objectstore.Open(objectstore.Config{
		Endpoint: h.cfg.S3.Endpoint, Bucket: h.cfg.S3.Bucket, AccessKey: h.cfg.S3.AccessKey,
		SecretKey: h.cfg.S3.SecretKey, Region: h.cfg.S3.Region, UseSSL: h.cfg.S3.UseSSL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.(*objectstore.S3Store).EnsureBucket(context.Background()); err != nil {
		t.Fatalf("make the bucket: %v", err)
	}
	accounts := auth.NewService(h.cluster, cheapPasswords(), time.Hour)
	a := &apiServer{accounts: accounts, store: store, themes: theme.NewService(h.cluster, store), h: h}
	pages := page.NewService(h.cluster)
	a.attachments = attachment.NewService(h.cluster, store, pages).WithMaxSize(testUploadLimit).WithLogger(discard())
	server := &httpapi.Server{
		DB: h.cluster, Log: discard(), Auth: accounts, Accounts: accounts, Themes: a.themes,
		Spaces: space.NewService(h.cluster), Pages: pages, Attachments: a.attachments, Perms: perm.NewService(h.cluster), Search: search.NewService(h.cluster),
		Labels: label.NewService(h.cluster, pages), Comments: comment.NewService(h.cluster), Reactions: reaction.NewService(h.cluster), Watches: watch.NewService(h.cluster), Notifications: notify.NewService(h.cluster), Stars: star.NewService(h.cluster), Home: home.NewService(h.cluster), Stale: stale.NewService(h.cluster),
		Fresh: h.freshness(t), CookieName: h.cfg.Auth.SessionCookie, Armature: h.armature(t),
		Audit: audit.NewService(h.cluster), AuditRetention: config.DefaultRetainAudit,
	}
	a.srv = httptest.NewServer(observed(t, server.Routes(nil)))
	t.Cleanup(a.srv.Close)
	return a
}

// freshness is the read-your-writes store the running api uses, under a
// prefix of the suite's own.
func (h *harness) freshness(t *testing.T) *freshness.ValkeyTracker {
	t.Helper()
	if h.cfg.Valkey.URL == "" {
		t.Fatal("STATOR_VALKEY_URL is not set; run the suite with make test-integration against the running stack")
	}
	opts, err := redis.ParseURL(h.cfg.Valkey.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	tracker := freshness.NewValkeyTracker(client, h.cfg.DB.ReadYourWritesTTL, "stator-test:ryw:")
	if err := tracker.Ping(t.Context()); err != nil {
		t.Fatalf("reach Valkey: %v", err)
	}
	return tracker
}

// client is somebody calling the API from a browser of their own, whose
// cookies it keeps; an empty token is nobody at all.
type client struct {
	api   *apiServer
	token string
	user  uuid.UUID
	ctx   context.Context
	http  *http.Client
	// eager reads without waiting for anybody else's write to replicate.
	eager bool
	// bearer is a personal access token sent in place of any cookie.
	bearer string
}

func cookieJarClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// as opens a session for a member in their organization, the row a sign-in
// writes, and hands its cookie to a browser of their own.
func (a *apiServer) as(t *testing.T, user, org uuid.UUID, slug string) *client {
	t.Helper()
	var token string
	_, err := a.h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		_, token, err = auth.OpenSession(ctx, tx, user, &org, auth.ProofPassword, time.Now().Add(time.Hour), "", "")
		return err
	})
	if err != nil {
		t.Fatalf("open a session: %v", err)
	}
	o := tenant.Org{ID: org, Slug: slug}
	c := &client{api: a, token: token, user: user, ctx: db.WithUser(tenant.WithOrg(context.Background(), o), user), http: cookieJarClient()}
	base, _ := url.Parse(a.srv.URL)
	c.http.Jar.SetCookies(base, []*http.Cookie{{Name: a.h.cfg.Auth.SessionCookie, Value: token, Path: "/"}})
	return c
}

func (a *apiServer) anonymous() *client { return &client{api: a, http: cookieJarClient()} }

// withToken is a script calling the API with a personal access token and no
// cookies at all.
func (a *apiServer) withToken(secret string) *client {
	return &client{api: a, bearer: secret, http: &http.Client{}}
}

type response struct {
	Status int
	Body   map[string]any
	Raw    []byte
}

func (c *client) send(t *testing.T, method, path, contentType string, body io.Reader) response {
	t.Helper()
	req, err := http.NewRequest(method, c.api.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	c.api.handOver(t, c, req.Method)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode, Raw: raw}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func (c *client) call(t *testing.T, method, path string, body any) response {
	t.Helper()
	if body == nil {
		return c.send(t, method, path, "", nil)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return c.send(t, method, path, "application/json", bytes.NewReader(encoded))
}

func (c *client) get(t *testing.T, path string) response { return c.call(t, http.MethodGet, path, nil) }
func (c *client) post(t *testing.T, path string, body any) response {
	return c.call(t, http.MethodPost, path, body)
}
func (c *client) patch(t *testing.T, path string, body any) response {
	return c.call(t, http.MethodPatch, path, body)
}
func (c *client) put(t *testing.T, path string, body any) response {
	return c.call(t, http.MethodPut, path, body)
}
func (c *client) delete(t *testing.T, path string) response {
	return c.call(t, http.MethodDelete, path, nil)
}

// upload sends a file as a multipart part named file, the way the web does.
func (c *client) upload(t *testing.T, path, name string, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	return c.send(t, http.MethodPost, path, form.FormDataContentType(), &body)
}

// download fetches a file and keeps the headers, which say how it is served.
func (c *client) download(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.api.srv.URL+path, nil)
	c.api.handOver(t, c, req.Method)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

// want fails the test unless the answer has the status, and returns it.
func want(t *testing.T, got response, status int, what string) response {
	t.Helper()
	if got.Status != status {
		t.Fatalf("%s: got %d, want %d: %s", what, got.Status, status, got.Raw)
	}
	return got
}

// obj walks into the body by keys, failing when a step is not an object.
func obj(t *testing.T, r response, keys ...string) map[string]any {
	t.Helper()
	var cur any = r.Body
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%v is not an object in %s", keys, r.Raw)
		}
		cur = m[k]
	}
	m, ok := cur.(map[string]any)
	if !ok {
		t.Fatalf("%v is not an object in %s", keys, r.Raw)
	}
	return m
}

func list(t *testing.T, r response, key string) []any {
	t.Helper()
	l, ok := r.Body[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list in %s", key, r.Raw)
	}
	return l
}

// errorCode reads the envelope and checks it is a sentence, as every refusal is.
func errorCode(t *testing.T, r response) string {
	t.Helper()
	e, ok := r.Body["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope in %s", r.Raw)
	}
	if msg, _ := e["message"].(string); msg == "" || msg[len(msg)-1] != '.' {
		t.Errorf("the message %q is not a sentence", msg)
	}
	code, _ := e["code"].(string)
	return code
}

// addPerson makes somebody else a member of an organization, behind the
// policies' back like makeMember.
func (h *harness) addPerson(t *testing.T, org uuid.UUID, role string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	email := fmt.Sprintf("%s-%s@example.test", role, uuid.NewString()[:8])
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`, email, "A "+role).Scan(&id); err != nil {
		t.Fatalf("create a %s: %v", role, err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)`, org, id, role); err != nil {
		t.Fatalf("make the %s a member: %v", role, err)
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE id = $1`, id) })
	h.settle(t)
	return id
}

// slugOf is an organization's slug, which a principal carries.
func (h *harness) slugOf(t *testing.T, org uuid.UUID) string {
	t.Helper()
	var slug string
	if err := h.super.QueryRow(context.Background(), `SELECT slug FROM org WHERE id = $1`, org).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	return slug
}
