//go:build integration

package test

import (
	"context"
	"testing"
)

// A backup restores with an empty search_path, so a PL/pgSQL function that
// a generated column or a check calls must not depend on the caller's. A
// CREATE OR REPLACE drops the pin unless it repeats it, as 00690 explains.
func TestEveryPLpgSQLFunctionPinsItsSearchPath(t *testing.T) {
	h := newHarness(t)
	rows, err := h.super.Query(context.Background(), `
		SELECT p.oid::regprocedure::text
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_language l ON l.oid = p.prolang
		WHERE n.nspname = 'public' AND l.lanname = 'plpgsql'
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')
		  AND NOT EXISTS (SELECT 1 FROM unnest(p.proconfig) c WHERE c LIKE 'search_path=%')
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s does not pin its search_path; add SET search_path = public, pg_temp to its definition", name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
