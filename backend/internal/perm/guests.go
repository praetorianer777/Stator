package perm

import (
	"errors"
	"slices"

	"github.com/jackc/pgx/v5/pgconn"
)

// GuestConstraints are the database's names for its refusals of what would
// take a guest beyond their one space; each refusal is a sentence.
var GuestConstraints = []string{
	"space_grant_guest_one_space",
	"space_grant_guest_not_administrator",
	"group_member_not_guest",
	"org_member_guest_fixed",
}

// GuestRefusal is the sentence the database refused a guest's widening with,
// and whether err is such a refusal.
func GuestRefusal(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && slices.Contains(GuestConstraints, pgErr.ConstraintName) {
		return pgErr.Message, true
	}
	return "", false
}
