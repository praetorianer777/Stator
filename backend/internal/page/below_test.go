package page

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReadingOrderPutsEachPageAfterItsParentInSortOrder(t *testing.T) {
	root := uuid.New()
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	mk := func(title string, parent uuid.UUID, depth int, rank string, updated time.Time) BelowPage {
		return BelowPage{ID: uuid.New(), ParentID: parent, Title: title, Depth: depth, rank: rank, UpdatedAt: updated}
	}
	zulu := mk("zulu", root, 1, "a", day(3))
	alpha := mk("Alpha", root, 1, "b", day(1))
	mike := mk("mike", root, 1, "c", day(2))
	under := mk("beta", zulu.ID, 2, "a", day(9))
	under2 := mk("Able", zulu.ID, 2, "b", day(5))
	// The query hands them over breadth first.
	pages := []BelowPage{zulu, alpha, mike, under, under2}
	titles := func(ps []BelowPage) []string {
		out := make([]string, len(ps))
		for i, p := range ps {
			out[i] = p.Title
		}
		return out
	}
	for sort, want := range map[string][]string{
		"tree":    {"zulu", "beta", "Able", "Alpha", "mike"},
		"title":   {"Alpha", "mike", "zulu", "Able", "beta"},
		"updated": {"zulu", "beta", "Able", "mike", "Alpha"},
	} {
		if got := titles(readingOrder(slices.Clone(pages), root, sort)); !slices.Equal(got, want) {
			t.Errorf("sorted by %s: %v, want %v", sort, got, want)
		}
	}
}

func TestBelowQueryTakesWhatABlockHolds(t *testing.T) {
	depth := func(n int) *int { return &n }
	for _, ok := range []BelowQuery{
		{Scope: "children", Sort: "tree"},
		{Scope: "subtree", Sort: "title", Depth: depth(1)},
		{Scope: "subtree", Sort: "updated", Depth: depth(10)},
	} {
		if err := ok.Check(); err != nil {
			t.Errorf("%+v was refused: %v", ok, err)
		}
	}
	for want, bad := range map[error]BelowQuery{
		ErrBadScope: {Scope: "space", Sort: "tree"},
		ErrBadSort:  {Scope: "children", Sort: "created"},
		ErrBadDepth: {Scope: "subtree", Sort: "tree", Depth: depth(11)},
	} {
		if err := bad.Check(); !errors.Is(err, want) {
			t.Errorf("%+v: %v, want %v", bad, err, want)
		}
	}
	if err := (BelowQuery{Scope: "subtree", Sort: "tree", Depth: depth(0)}).Check(); !errors.Is(err, ErrBadDepth) {
		t.Errorf("depth 0: %v", err)
	}
}
