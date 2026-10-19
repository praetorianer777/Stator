package oauth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
)

// The partner apps, kept by administrators under row level security.

const clientColumns = `c.id, c.name, c.scopes, c.keycloak_client_id, c.created_at, c.updated_at,
	(SELECT count(*)::integer FROM api_token t WHERE t.client_id = c.id AND t.expires_at > now())`

func scanClient(row pgx.Row) (*Client, error) {
	var c Client
	if err := row.Scan(&c.ID, &c.Name, &c.Scopes, &c.KeycloakClientID, &c.CreatedAt, &c.UpdatedAt, &c.Grants); err != nil {
		return nil, err
	}
	return &c, nil
}

// Clients lists the apps by name.
func (s *Service) Clients(ctx context.Context) ([]Client, error) {
	out := []Client{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT `+clientColumns+` FROM oauth_client c ORDER BY lower(c.name), c.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanClient(rows)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return rows.Err()
	})
	return out, err
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxNameLength {
		return "", &FieldError{Field: "name", Message: fmt.Sprintf("Name the app in 1 to %d characters, as people will read it in their token list.", MaxNameLength)}
	}
	return name, nil
}

func validKeycloakClient(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > MaxKeycloakClientIDLength || strings.ContainsAny(id, " \t\r\n") {
		return "", &FieldError{Field: "keycloakClientId", Message: "Name the identity provider's client the app signs people in with, such as exciter in the realm dynamo."}
	}
	return id, nil
}

// taken is the database refusing a second app for one Keycloak client.
func taken(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName == "oauth_client_keycloak_client_id_key" {
		return &FieldError{Field: "keycloakClientId", Message: "Another connected app names this Keycloak client already. Change that app, or name another client."}
	}
	return err
}

// CreateClient registers an app and answers its secret, once.
func (s *Service) CreateClient(ctx context.Context, in ClientInput, ip string) (*Client, db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	name, err := validName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	scopes, err := normalScopes("scopes", in.Scopes)
	if err != nil {
		return nil, 0, err
	}
	keycloak, err := validKeycloakClient(in.KeycloakClientID)
	if err != nil {
		return nil, 0, err
	}
	secret, digest, err := auth.GeneratePrefixed(auth.ClientSecretPrefix)
	if err != nil {
		return nil, 0, err
	}
	var out *Client
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO oauth_client (name, scopes, keycloak_client_id, secret_hash, created_by)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`, name, scopes, keycloak, digest, actor).Scan(&id); err != nil {
			return taken(err)
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthClientCreated, TargetType: "oauth_client", TargetID: &id, Actor: actor, IP: ip,
			Data: map[string]any{"name": name, "scopes": scopes, "keycloakClientId": keycloak}}); err != nil {
			return err
		}
		var err error
		out, err = scanClient(tx.QueryRow(ctx, `SELECT `+clientColumns+` FROM oauth_client c WHERE c.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, lsn, err
	}
	out.Secret = secret
	return out, lsn, nil
}

// UpdateClient changes an app. A new name follows into its grants' names,
// and narrower scopes narrow every grant at once.
func (s *Service) UpdateClient(ctx context.Context, id uuid.UUID, in ClientChanges, ip string) (*Client, db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	changed := map[string]any{}
	var (
		name, keycloak *string
		scopes         []string
	)
	if in.Name != nil {
		v, err := validName(*in.Name)
		if err != nil {
			return nil, 0, err
		}
		name, changed["name"] = &v, v
	}
	if in.Scopes != nil {
		v, err := normalScopes("scopes", *in.Scopes)
		if err != nil {
			return nil, 0, err
		}
		scopes, changed["scopes"] = v, v
	}
	if in.KeycloakClientID != nil {
		v, err := validKeycloakClient(*in.KeycloakClientID)
		if err != nil {
			return nil, 0, err
		}
		keycloak, changed["keycloakClientId"] = &v, v
	}
	var out *Client
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE oauth_client SET name = coalesce($2::text, name), scopes = coalesce($3::text[], scopes),
			    keycloak_client_id = coalesce($4::text, keycloak_client_id)
			WHERE id = $1`, id, name, scopes, keycloak)
		if err != nil {
			return taken(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSuchClient
		}
		if err := audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthClientUpdated, TargetType: "oauth_client", TargetID: &id, Actor: actor, IP: ip,
			Data: changed}); err != nil {
			return err
		}
		out, err = scanClient(tx.QueryRow(ctx, `SELECT `+clientColumns+` FROM oauth_client c WHERE c.id = $1`, id))
		return err
	})
	if err != nil || (name == nil && scopes == nil) {
		return out, lsn, err
	}
	// The grants are the people's own rows, which only the admin role may
	// change for them; the administrator's write above decided it.
	followed, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if name != nil {
			if _, err := tx.Exec(ctx, `UPDATE api_token SET name = $2 WHERE client_id = $1`, id, *name+auth.ConnectedAppSuffix); err != nil {
				return err
			}
		}
		// Only read alone narrows: a grant that may write was given write.
		if scopes != nil && !slices.Contains(scopes, ScopeWrite) {
			if _, err := tx.Exec(ctx, `UPDATE api_token SET scopes = $2::text[] WHERE client_id = $1 AND scopes = '{}'`,
				id, tokenScopes(scopes)); err != nil {
				return err
			}
		}
		return nil
	})
	return out, max(lsn, followed), err
}

// DeleteClient removes an app; every grant of it ends with it, by the
// foreign key.
func (s *Service) DeleteClient(ctx context.Context, id uuid.UUID, ip string) (db.LSN, error) {
	actor, _ := db.UserFrom(ctx)
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			name   string
			grants int
		)
		err := tx.QueryRow(ctx, `SELECT name, (SELECT count(*)::integer FROM api_token WHERE client_id = $1) FROM oauth_client WHERE id = $1`, id).
			Scan(&name, &grants)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSuchClient
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_client WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{Action: audit.ActionOAuthClientRemoved, TargetType: "oauth_client", TargetID: &id, Actor: actor, IP: ip,
			Data: map[string]any{"name": name, "grantsEnded": grants}})
	})
}
