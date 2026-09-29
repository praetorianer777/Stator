// Package auth names who is calling. Resolving a credential to a principal is
// the sign-in work still to come; the HTTP layer already carries the result.
package auth

import (
	"errors"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// ErrInvalidToken is an unknown, expired or revoked credential. The caller is
// then anonymous, which is not an error in itself.
var ErrInvalidToken = errors.New("invalid or expired credential")

// Principal is the caller a credential resolved to.
type Principal struct {
	UserID uuid.UUID
	// Org is the organization the caller is acting in; nil until one is chosen.
	Org *tenant.Org
}

// InOrg reports whether the caller has chosen an organization to act in.
func (p *Principal) InOrg() bool { return p != nil && p.Org != nil && p.Org.ID != uuid.Nil }
