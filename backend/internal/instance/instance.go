// Package instance finds the one organization an instance serves, so nothing
// has to read it from an address or a setting.
package instance

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
)

var (
	// ErrNone is answered before the bootstrap has made the organization.
	ErrNone = errors.New("this instance has no organization yet; run seed bootstrap to make it")
	// ErrSeveral is answered for a database the upgrade should have refused.
	ErrSeveral = errors.New("this database holds more than one organization, and Stator serves one per instance; export every organization, delete all but one, then start Stator again")
)

// Org names the instance's organization.
type Org struct {
	ID   uuid.UUID
	Slug string
}

// Find reads the one organization through tx, which must see every row.
func Find(ctx context.Context, tx db.DBTX) (Org, error) {
	rows, err := tx.Query(ctx, `SELECT id, slug::text FROM org LIMIT 2`)
	if err != nil {
		return Org{}, fmt.Errorf("find the organization: %w", err)
	}
	defer rows.Close()
	var found []Org
	for rows.Next() {
		var org Org
		if err := rows.Scan(&org.ID, &org.Slug); err != nil {
			return Org{}, err
		}
		found = append(found, org)
	}
	if err := rows.Err(); err != nil {
		return Org{}, err
	}
	switch len(found) {
	case 0:
		return Org{}, ErrNone
	case 1:
		return found[0], nil
	default:
		return Org{}, ErrSeveral
	}
}

// ID reads the organization's id through tx, for what is still keyed or
// sealed by it. tx must be able to see org, which an anonymous reader cannot.
func ID(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM org`).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNone
		}
		return uuid.Nil, fmt.Errorf("read the organization: %w", err)
	}
	return id, nil
}

// Resolver finds the organization once and keeps it: neither its id nor its
// slug changes while the process runs.
type Resolver struct {
	cluster *db.Cluster
	mu      sync.Mutex
	org     *Org
}

func NewResolver(cluster *db.Cluster) *Resolver { return &Resolver{cluster: cluster} }

// Org is the instance's organization. Until the bootstrap has made it, every
// call asks again, so a process started first finds it afterwards.
func (r *Resolver) Org(ctx context.Context) (Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.org != nil {
		return *r.org, nil
	}
	var org Org
	err := r.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		org, err = Find(ctx, tx)
		return err
	})
	if err != nil {
		return Org{}, err
	}
	r.org = &org
	return org, nil
}
