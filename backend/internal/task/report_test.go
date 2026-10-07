package task

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func TestCheckReportReadsTheAssigneeAndRefusesWhatAReportCannotDo(t *testing.T) {
	good := ReportInput{Due: document.DueAny, State: document.TaskStateOpen, Limit: document.DefaultReportedTasks}
	id := uuid.New()
	for _, assignee := range []string{"", document.AssigneeReader, document.AssigneeNobody, id.String()} {
		in := good
		in.Assignee = assignee
		person, err := checkReport(in)
		if err != nil || (assignee == id.String()) != (person == id) {
			t.Errorf("the assignee %q read as %v: %v", assignee, person, err)
		}
	}
	for field, change := range map[string]func(*ReportInput){
		"assignee": func(in *ReportInput) { in.Assignee = "ann" },
		"due":      func(in *ReportInput) { in.Due = "later" },
		"state":    func(in *ReportInput) { in.State = "half" },
		"limit":    func(in *ReportInput) { in.Limit = document.MaxReportedTasks + 1 },
	} {
		in := good
		change(&in)
		var refused *FieldError
		if _, err := checkReport(in); !errors.As(err, &refused) || refused.Field != field || refused.Message == "" {
			t.Errorf("%s: %v", field, err)
		}
	}
	// An id written another way than the allowlist takes is no id a block may hold.
	upper := good
	upper.Assignee = "0199A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A5B"
	if _, err := checkReport(upper); err == nil {
		t.Error("an id in capitals was taken")
	}
	for _, limit := range []int{0, 1, document.MaxReportedTasks} {
		in := good
		in.Limit = limit
		if _, err := checkReport(in); (err == nil) != (limit > 0) {
			t.Errorf("a limit of %d: %v", limit, err)
		}
	}
	for _, due := range document.TaskReportDues {
		if _, ok := dueFilters[due]; !ok {
			t.Errorf("the due day %q adds nothing to the query", due)
		}
	}
}
