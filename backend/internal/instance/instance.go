// Package instance finds the one organization an instance serves, so nothing
// has to read it from an address or a setting.
package instance

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

var (
	// ErrNone is answered before the bootstrap has made the organization.
	ErrNone = errors.New("this instance has no organization yet; run seed bootstrap to make it")
	// ErrSeveral is answered for a database the upgrade should have refused.
	ErrSeveral = errors.New("this database holds more than one organization, and Stator serves one per instance; export every organization, delete all but one, then start Stator again")
)

// Find reads the one organization through tx, which must see every row.
func Find(ctx context.Context, tx db.DBTX) (tenant.Org, error) {
	rows, err := tx.Query(ctx, `SELECT id, slug::text FROM org LIMIT 2`)
	if err != nil {
		return tenant.Org{}, fmt.Errorf("find the organization: %w", err)
	}
	defer rows.Close()
	var found []tenant.Org
	for rows.Next() {
		var org tenant.Org
		if err := rows.Scan(&org.ID, &org.Slug); err != nil {
			return tenant.Org{}, err
		}
		found = append(found, org)
	}
	if err := rows.Err(); err != nil {
		return tenant.Org{}, err
	}
	switch len(found) {
	case 0:
		return tenant.Org{}, ErrNone
	case 1:
		return found[0], nil
	default:
		return tenant.Org{}, ErrSeveral
	}
}

// Resolver finds the organization once and keeps it: neither its id nor its
// slug changes while the process runs.
type Resolver struct {
	cluster *db.Cluster
	mu      sync.Mutex
	org     *tenant.Org
}

func NewResolver(cluster *db.Cluster) *Resolver { return &Resolver{cluster: cluster} }

// Org is the instance's organization. Until the bootstrap has made it, every
// call asks again, so a process started first finds it afterwards.
func (r *Resolver) Org(ctx context.Context) (tenant.Org, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.org != nil {
		return *r.org, nil
	}
	var org tenant.Org
	err := r.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		org, err = Find(ctx, tx)
		return err
	})
	if err != nil {
		return tenant.Org{}, err
	}
	r.org = &org
	return org, nil
}
