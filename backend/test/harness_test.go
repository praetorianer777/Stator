//go:build integration

// Package test holds the integration suite. It runs against a real Postgres,
// because row level security is a property of the database that cannot be faked.
package test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// Environment the harness needs, set by make test-integration.
const (
	envPrimary   = "STATOR_DB_PRIMARY_URL"
	envSuperuser = "STATOR_TEST_SUPERUSER_URL"
)

// harness bundles the cluster under test, connected as stator_app and
// stator_admin, with a superuser pool used only to arrange and clean up.
type harness struct {
	cfg     config.Config
	cluster *db.Cluster
	super   *pgxpool.Pool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if os.Getenv(envPrimary) == "" || os.Getenv(envSuperuser) == "" {
		t.Skipf("%s and %s are not set; run the suite with make test-integration", envPrimary, envSuperuser)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cluster, err := db.Open(ctx, cfg.DB, log)
	if err != nil {
		t.Fatalf("open cluster: %v", err)
	}
	super, err := pgxpool.New(ctx, os.Getenv(envSuperuser))
	if err != nil {
		cluster.Close()
		t.Fatalf("connect superuser: %v", err)
	}
	t.Cleanup(func() {
		super.Close()
		cluster.Close()
	})
	return &harness{cfg: cfg, cluster: cluster, super: super}
}

// member is one organization with one person in it, made behind the policies'
// back so that what a test observes is only the policies' doing.
type member struct {
	org  uuid.UUID
	user uuid.UUID
	ctx  context.Context
}

func (h *harness) makeMember(t *testing.T, slug string) member {
	t.Helper()
	ctx := context.Background()
	// Unique per run so repeated runs against a kept database do not collide.
	suffix := uuid.NewString()[:8]
	full := fmt.Sprintf("%s-%s", slug, suffix)

	var m member
	if err := h.super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, full, slug).Scan(&m.org); err != nil {
		t.Fatalf("create org %s: %v", full, err)
	}
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`,
		full+"@example.test", "Person of "+slug).Scan(&m.user); err != nil {
		t.Fatalf("create user in %s: %v", full, err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')`, m.org, m.user); err != nil {
		t.Fatalf("make the user a member of %s: %v", full, err)
	}
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM org WHERE id = $1`, m.org)
		_, _ = h.super.Exec(context.Background(), `DELETE FROM app_user WHERE id = $1`, m.user)
	})
	m.ctx = tenant.WithOrg(ctx, tenant.Org{ID: m.org, Slug: full})
	return m
}

// count runs a count(*) query in ctx's tenant scope on the primary.
func (h *harness) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	err := h.cluster.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, sql, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func isNoTenant(err error) bool { return errors.Is(err, tenant.ErrNoTenant) }
