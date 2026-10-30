//go:build integration

package test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/brand"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/instance"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/secret"
	"github.com/praetorianer777/stator/backend/internal/theme"
	"github.com/praetorianer777/stator/backend/internal/webhook"
)

// dropOrgMigration is the migration that takes the organization out of the
// schema (#380).
const dropOrgMigration = "_drop_org_id.sql"

// An instance written by the release before the schema lost the organization
// upgrades whole: its page, its file, theme file and logo are found under the
// keys they were stored with, and the secrets sealed with the organization's
// id still open. A down migration then brings org_id back filled with the one
// organization, and the upgrade runs again over it.
func TestAnInstanceUpgradesAcrossTheSchemaChangeWhole(t *testing.T) {
	first := newHarness(t)
	ctx := context.Background()
	dropping, before := migrationsAround(t, dropOrgMigration)
	scratch := scratchDatabase(t)
	upTo(t, scratch.db, before)

	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	store := testStore(t, first)
	put := func(key, body string) {
		t.Helper()
		if err := store.Put(ctx, key, strings.NewReader(body), int64(len(body)), "application/octet-stream"); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
		t.Cleanup(func() { _ = store.Delete(context.Background(), key) })
	}
	one := func(sql string, args ...any) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := scratch.pool.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id
	}

	// Written as the release before wrote it: org_id filled by its default,
	// keys under the organization, secrets sealed with its id.
	org := one(`INSERT INTO org (slug, name) VALUES ('acme', 'Acme') RETURNING id`)
	owner := one(`INSERT INTO app_user (email, name) VALUES ('ada@acme.test', 'Ada') RETURNING id`)
	one(`INSERT INTO org_member (user_id, org_role) VALUES ($1, 'owner') RETURNING user_id`, owner)
	space := one(`INSERT INTO space (key, name, created_by) VALUES ('KEPT', 'Kept', $1) RETURNING id`, owner)
	pageID := one(`INSERT INTO page (space_id, rank, title, created_by, updated_by) VALUES ($1, 'a', 'Kept page', $2, $2) RETURNING id`, space, owner)

	var fileID uuid.UUID
	var fileKey string
	if err := scratch.pool.QueryRow(ctx, `
		INSERT INTO attachment (page_id, uploaded_by, file_name, content_type, size_bytes)
		VALUES ($1, $2, 'plan.txt', 'text/plain', 9) RETURNING id, object_key`, pageID, owner).Scan(&fileID, &fileKey); err != nil {
		t.Fatal(err)
	}
	if want := "org/" + org.String() + "/page/" + pageID.String() + "/" + fileID.String(); fileKey != want {
		t.Fatalf("the release before stored the file under %s, want %s", fileKey, want)
	}
	put(fileKey, "old plans")

	themeID := one(`INSERT INTO theme (owner_id, name) VALUES ($1, 'Kept') RETURNING id`, owner)
	assetID := one(`INSERT INTO theme_asset (theme_id, name, content_type, size) VALUES ($1, 'font.woff2', 'font/woff2', 9) RETURNING id`, themeID)
	put("org/"+org.String()+"/theme/"+themeID.String()+"/"+assetID.String(), "old glyph")

	one(`INSERT INTO org_brand (logo_type, logo_size, logo_version) VALUES ('image/png', 8, 3) RETURNING org_id`)
	put("org/"+org.String()+"/brand/logo-3", "old logo")

	bin, address := hookBin(t)
	endpoint := uuid.New()
	sealedHook, err := box.Seal([]byte("whsec_kept"), []byte("webhook:"+org.String()+":"+endpoint.String()))
	if err != nil {
		t.Fatal(err)
	}
	one(`INSERT INTO webhook_endpoint (id, org_id, name, url, secret_sealed, topics, owner_id) VALUES ($1, $2, 'Kept', $3, $4, '{*}', $5) RETURNING id`,
		endpoint, org, address, sealedHook, owner)

	sealedArmature, err := box.Seal([]byte("armature_kept"), append([]byte("armature.webhook:"), org[:]...))
	if err != nil {
		t.Fatal(err)
	}
	one(`INSERT INTO armature_connection (base_url, org_slug, webhook_secret) VALUES ('http://armature.test', 'acme', $1) RETURNING org_id`, sealedArmature)
	sealedClient, err := box.Seal([]byte("client_kept"), org[:])
	if err != nil {
		t.Fatal(err)
	}
	one(`INSERT INTO oidc_provider (issuer, client_id, client_secret) VALUES ('https://id.acme.test', 'stator', $1) RETURNING org_id`, sealedClient)

	if err := goose.UpContext(ctx, scratch.db, ".", goose.WithAllowMissing()); err != nil {
		t.Fatalf("the upgrade: %v", err)
	}
	if v := dbVersion(t, scratch.db); v < dropping {
		t.Fatalf("the upgrade stopped at %d, before %d", v, dropping)
	}

	// Reads go to the replica, which must have the scratch database first.
	if replica := os.Getenv(envReplicaSuperuser); replica != "" {
		if err := waitForStandby(ctx, scratch.pool, replica); err != nil {
			t.Fatal(err)
		}
	}
	pointAt(t, scratch.name)
	h := connectHarness(t)
	actor := perm.Actor{UserID: owner, Role: auth.RoleMember}
	as := db.WithUser(ctx, owner)

	site, err := instance.NewResolver(h.cluster).Site(ctx)
	if err != nil || site.ID != org || site.Slug != "acme" {
		t.Fatalf("the site is %+v (%v), want the organization %s it was", site, err, org)
	}

	files := attachment.NewService(h.cluster, store, page.NewService(h.cluster))
	_, body, err := files.Open(as, actor, fileID)
	if err != nil {
		t.Fatalf("open the file stored before: %v", err)
	}
	if got := readString(t, body); got != "old plans" {
		t.Errorf("the file reads %q, want the bytes stored before", got)
	}

	glyph, _, err := theme.NewService(h.cluster, store).OpenAsset(as, themeID, assetID, owner)
	if err != nil {
		t.Fatalf("open the theme file stored before: %v", err)
	}
	if got := readString(t, glyph); got != "old glyph" {
		t.Errorf("the theme file reads %q, want the bytes stored before", got)
	}

	logo, _, err := brand.NewService(h.cluster, store).OpenLogo(as)
	if err != nil {
		t.Fatalf("open the logo stored before: %v", err)
	}
	if got := readString(t, logo); got != "old logo" {
		t.Errorf("the logo reads %q, want the bytes stored before", got)
	}

	// The endpoint's secret is opened where a delivery is signed, so a test
	// delivery signed with it proves the seal survived.
	hooks := h.webhooks(t)
	if _, _, err := hooks.Test(as, endpoint); err != nil {
		t.Fatalf("send a test delivery: %v", err)
	}
	got := binDeliveries(t, bin)
	if len(got) != 1 || !webhook.Verify([]byte(got[0].Body), "whsec_kept", got[0].Headers["X-Stator-Signature-256"]) {
		t.Errorf("the delivery is not signed with the secret sealed before: %+v", got)
	}
	// An endpoint from before the envelope had a number is on version 1,
	// which is what it was sent all along.
	if len(got) == 1 && got[0].Headers[webhook.VersionHeader] != "1" {
		t.Errorf("an endpoint kept from before is sent version %q, want 1", got[0].Headers[webhook.VersionHeader])
	}

	var armatureSealed, clientSealed []byte
	if err := scratch.pool.QueryRow(ctx, `SELECT (SELECT webhook_secret FROM armature_connection), (SELECT client_secret FROM oidc_provider)`).Scan(&armatureSealed, &clientSealed); err != nil {
		t.Fatal(err)
	}
	if plain, err := armature.NewService(h.cluster, box, nil, nil, armature.Options{Log: discard()}).OpenWebhookSecret(site.ID, armatureSealed); err != nil || plain != "armature_kept" {
		t.Errorf("the Armature webhook secret opens to %q (%v)", plain, err)
	}
	if plain, err := box.Open(clientSealed, site.ID[:]); err != nil || string(plain) != "client_kept" {
		t.Errorf("the sign-in client secret opens to %q (%v)", plain, err)
	}

	// A file stored now takes a key without the organization.
	var newKey string
	if err := scratch.pool.QueryRow(ctx, `
		INSERT INTO attachment (page_id, uploaded_by, file_name, content_type, size_bytes)
		VALUES ($1, $2, 'new.txt', 'text/plain', 3) RETURNING object_key`, pageID, owner).Scan(&newKey); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(newKey, "page/"+pageID.String()+"/") {
		t.Errorf("a new file is stored under %s, want a key without the organization", newKey)
	}

	h.cluster.Close()
	if err := goose.DownToContext(ctx, scratch.db, ".", before); err != nil {
		t.Fatalf("the down migration: %v", err)
	}
	var orgs, filled int
	if err := scratch.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM org), (SELECT count(*) FROM page)`).Scan(&orgs, &filled); err != nil {
		t.Fatalf("read the downgraded schema: %v", err)
	}
	if orgs != 1 || filled != 1 {
		t.Errorf("after the down migration: %d organizations, %d pages of it, want 1 each", orgs, filled)
	}
	if err := goose.UpContext(ctx, scratch.db, ".", goose.WithAllowMissing()); err != nil {
		t.Fatalf("the upgrade after the down migration: %v", err)
	}
}

// testStore is the suite's bucket, as cmd/api opens it.
func testStore(t *testing.T, h *harness) objectstore.Store {
	t.Helper()
	cfg := h.cfg.S3
	store, err := objectstore.Open(objectstore.Config{
		Endpoint: cfg.Endpoint, Bucket: cfg.Bucket, AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey, Region: cfg.Region, UseSSL: cfg.UseSSL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.(*objectstore.S3Store).EnsureBucket(context.Background()); err != nil {
		t.Fatalf("make the bucket: %v", err)
	}
	return store
}

func readString(t *testing.T, r io.ReadCloser) string {
	t.Helper()
	defer r.Close()
	var b bytes.Buffer
	if _, err := io.Copy(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
