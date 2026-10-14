// Package instance finds the one site an instance serves, so nothing has to
// read it from an address or a setting.
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
	// ErrNone is answered before the bootstrap has made the site.
	ErrNone = errors.New("this instance has no organization yet; run seed bootstrap to make it")
	// ErrSeveral is answered for a database the upgrade should have refused.
	ErrSeveral = errors.New("this database holds more than one organization, and Stator serves one per instance; export every organization, delete all but one, then start Stator again")
)

// Site names the instance's site. Its id is what secrets sealed before the
// organization left the schema were sealed with, and still are; the slug is
// what old public addresses and the Armature webhook alias spell.
type Site struct {
	ID   uuid.UUID
	Slug string
}

// Find reads the site through tx, which must see every row.
func Find(ctx context.Context, tx db.DBTX) (Site, error) {
	rows, err := tx.Query(ctx, `SELECT id, slug::text FROM site LIMIT 2`)
	if err != nil {
		return Site{}, fmt.Errorf("find the site: %w", err)
	}
	defer rows.Close()
	var found []Site
	for rows.Next() {
		var site Site
		if err := rows.Scan(&site.ID, &site.Slug); err != nil {
			return Site{}, err
		}
		found = append(found, site)
	}
	if err := rows.Err(); err != nil {
		return Site{}, err
	}
	switch len(found) {
	case 0:
		return Site{}, ErrNone
	case 1:
		return found[0], nil
	default:
		return Site{}, ErrSeveral
	}
}

// ID reads the site's id through tx, for what is sealed with it. tx must be
// able to see site, which an anonymous reader cannot.
func ID(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM site`).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNone
		}
		return uuid.Nil, fmt.Errorf("read the site: %w", err)
	}
	return id, nil
}

// Resolver finds the site once and keeps it: neither its id nor its slug
// changes while the process runs.
type Resolver struct {
	cluster *db.Cluster
	mu      sync.Mutex
	site    *Site
}

func NewResolver(cluster *db.Cluster) *Resolver { return &Resolver{cluster: cluster} }

// Site is the instance's site. Until the bootstrap has made it, every call
// asks again, so a process started first finds it afterwards.
func (r *Resolver) Site(ctx context.Context) (Site, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.site != nil {
		return *r.site, nil
	}
	var site Site
	err := r.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		site, err = Find(ctx, tx)
		return err
	})
	if err != nil {
		return Site{}, err
	}
	r.site = &site
	return site, nil
}
