package task

import (
	"testing"

	"github.com/praetorianer777/stator/backend/internal/config"
)

// The setting's default and the watch's own are one number written twice,
// since config cannot import this package.
func TestTheDueIntervalAgreesWithTheSetting(t *testing.T) {
	if DefaultDueInterval != config.DefaultTaskDueCheck {
		t.Fatalf("task.DefaultDueInterval %s and config.DefaultTaskDueCheck %s disagree", DefaultDueInterval, config.DefaultTaskDueCheck)
	}
	if w := NewDueWatch(nil, nil, 0); w.interval != DefaultDueInterval {
		t.Errorf("a watch without an interval looks every %s", w.interval)
	}
}

// A task without a day sorts after every real one, in the cursor as in the
// index, which orders by the same day.
func TestADaylessTaskSortsLast(t *testing.T) {
	if got := noDay.Format("2006-01-02"); got != "9999-12-31" {
		t.Fatalf("a dayless task sorts at %s", got)
	}
}
