//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// The measurement that chose how tests stay apart (docs/decisions.md, #380):
//
//	make test-integration TESTFLAGS='-run=NONE -bench=Isolation -benchtime=20x'
//
// Each iteration is what one test costs before its own work starts and after
// it ends, beyond what both candidates share.
func BenchmarkIsolation(b *testing.B) {
	if os.Getenv(envPrimary) == "" || os.Getenv(envSuperuser) == "" {
		b.Skipf("%s and %s are not set; run the benchmark with make test-integration", envPrimary, envSuperuser)
	}
	ctx := context.Background()
	super, err := pgxpool.New(ctx, os.Getenv(envSuperuser))
	if err != nil {
		b.Fatal(err)
	}
	defer super.Close()

	start := time.Now()
	template, err := migratedTemplate(ctx, super)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("the template was ready after %s (migrated now, or found from an earlier run)", time.Since(start))

	for _, strategy := range []string{"WAL_LOG", "FILE_COPY"} {
		b.Run("cloneAndOpen/"+strategy, func(b *testing.B) {
			for b.Loop() {
				name := benchClone(b, super, template, strategy)
				cluster := benchOpen(b, name)
				benchPerson(b, cluster, uuid.Nil)
				cluster.Close()
				b.StopTimer()
				benchDrop(b, super, name)
				b.StartTimer()
			}
		})
		b.Run("drop/"+strategy, func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				name := benchClone(b, super, template, strategy)
				b.StartTimer()
				benchDrop(b, super, name)
			}
		})
	}

	b.Run("peopleInOneDatabase", func(b *testing.B) {
		name := benchClone(b, super, template, "WAL_LOG")
		defer benchDrop(b, super, name)
		shared := benchOpen(b, name)
		org := benchPerson(b, shared, uuid.Nil)
		shared.Close()
		for b.Loop() {
			cluster := benchOpen(b, name)
			for range 2 {
				benchPerson(b, cluster, org)
			}
			cluster.Close()
		}
	})
}

func benchClone(b *testing.B, super *pgxpool.Pool, template, strategy string) string {
	b.Helper()
	ctx := context.Background()
	suffix := make([]byte, cloneSuffixBytes)
	_, _ = rand.Read(suffix)
	name := testDatabasePrefix + hex.EncodeToString(suffix)
	if _, err := super.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s STRATEGY %s`,
		pgx.Identifier{name}.Sanitize(), pgx.Identifier{template}.Sanitize(), strategy)); err != nil {
		b.Fatal(err)
	}
	benchStandby(b, super)
	return name
}

func benchStandby(b *testing.B, super *pgxpool.Pool) {
	b.Helper()
	ctx := context.Background()
	var text string
	if err := super.QueryRow(ctx, `SELECT pg_logical_emit_message(false, 'stator-bench', '', true)::text`).Scan(&text); err != nil {
		b.Fatal(err)
	}
	lsn, _ := db.ParseLSN(text)
	replicaURL, err := databaseURL(os.Getenv(envReplicaSuperuser), "postgres")
	if err != nil || os.Getenv(envReplicaSuperuser) == "" {
		return
	}
	conn, err := pgx.Connect(ctx, replicaURL)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	for {
		var at string
		if err := conn.QueryRow(ctx, `SELECT COALESCE(pg_last_wal_replay_lsn(), '0/0')::text`).Scan(&at); err != nil {
			b.Fatal(err)
		}
		if got, err := db.ParseLSN(at); err == nil && got >= lsn {
			return
		}
		time.Sleep(replicaPollInterval)
	}
}

func benchOpen(b *testing.B, name string) *db.Cluster {
	b.Helper()
	cfg, err := config.Load()
	if err != nil {
		b.Fatal(err)
	}
	move := func(u string) string {
		m, err := databaseURL(u, name)
		if err != nil {
			b.Fatal(err)
		}
		return m
	}
	cfg.DB.PrimaryURL = move(cfg.DB.PrimaryURL)
	cfg.DB.AdminURL = move(cfg.DB.AdminURL)
	for i, u := range cfg.DB.ReplicaURLs {
		cfg.DB.ReplicaURLs[i] = move(strings.TrimSpace(u))
	}
	cluster, err := db.Open(context.Background(), cfg.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		b.Fatal(err)
	}
	return cluster
}

// benchPerson makes an organization with its owner when org is nil, and
// another person in org otherwise, the way the harness arranges a test.
func benchPerson(b *testing.B, cluster *db.Cluster, org uuid.UUID) uuid.UUID {
	b.Helper()
	ctx := context.Background()
	_, err := cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if org == uuid.Nil {
			if err := tx.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, 'Bench') RETURNING id`, "bench-"+uuid.NewString()[:8]).Scan(&org); err != nil {
				return err
			}
		}
		var user uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, 'Bench') RETURNING id`, uuid.NewString()+"@example.test").Scan(&user); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')`, org, user)
		return err
	})
	if err != nil {
		b.Fatal(err)
	}
	return org
}

func benchDrop(b *testing.B, super *pgxpool.Pool, name string) {
	b.Helper()
	if _, err := super.Exec(context.Background(), `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
		b.Fatal(err)
	}
}
