package home

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/keyset"
)

func at(u PageUpdate) keyset.Cursor { return keyset.Cursor{At: u.PublishedAt, ID: u.ID} }

func updates(n int) []PageUpdate {
	out := make([]PageUpdate, n)
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = PageUpdate{ID: uuid.New(), PublishedAt: start.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

func TestAWindowWithNothingAfterHasNoCursor(t *testing.T) {
	rows, next := window(updates(3), 3, at)
	if len(rows) != 3 || next != nil {
		t.Errorf("window kept %d rows and a cursor %v", len(rows), next)
	}
}

func TestAWindowWithMoreEndsAtItsLastRow(t *testing.T) {
	read := updates(4)
	rows, next := window(read, 3, at)
	if len(rows) != 3 {
		t.Fatalf("window kept %d rows, want 3", len(rows))
	}
	if next == nil {
		t.Fatal("a window with a row more has no cursor")
	}
	after, err := keyset.Decode(*next)
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != read[2].ID || !after.At.Equal(read[2].PublishedAt) {
		t.Errorf("the cursor points at %v, want the third row %v", after, read[2].ID)
	}
}
