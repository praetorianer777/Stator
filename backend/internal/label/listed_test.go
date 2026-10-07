package label

import (
	"errors"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func TestCheckListNormalizesLabelsAndRefusesWhatAListCannotDo(t *testing.T) {
	labels, err := checkList(ListInput{Labels: []string{"Release Notes", "release-notes", "v2"}, Match: document.MatchAny, Sort: document.SortTitle, Limit: 10})
	if err != nil || strings.Join(labels, ",") != "release-notes,v2" {
		t.Fatalf("%v %v", labels, err)
	}
	good := ListInput{Labels: []string{"a"}, Match: document.MatchAll, Sort: document.SortUpdated, Limit: 10}
	for field, change := range map[string]func(*ListInput){
		"label": func(in *ListInput) { in.Labels = nil },
		"match": func(in *ListInput) { in.Match = "some" },
		"sort":  func(in *ListInput) { in.Sort = "views" },
		"limit": func(in *ListInput) { in.Limit = 51 },
	} {
		in := good
		change(&in)
		var refused *FieldError
		if _, err := checkList(in); !errors.As(err, &refused) || refused.Field != field || refused.Message == "" {
			t.Errorf("%s: %v", field, err)
		}
	}
	tooMany := good
	tooMany.Labels = strings.Split("a b c d e f", " ")
	if _, err := checkList(tooMany); err == nil {
		t.Error("six labels were taken")
	}
	for _, limit := range []int{0, 1, 50} {
		in := good
		in.Limit = limit
		if _, err := checkList(in); (err == nil) != (limit > 0) {
			t.Errorf("a limit of %d: %v", limit, err)
		}
	}
}
