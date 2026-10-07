package armature

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCheckChartRefusesWhatArmatureWould(t *testing.T) {
	ok := []ChartInput{
		{Project: "CP", Query: "project = CP", Kind: ChartPie, GroupBy: "statusCategory"},
		{Project: "SEC2", Query: "assignee = currentUser()", Kind: ChartCreatedResolved, Days: MinChartDays},
		{Project: "CP", Query: "project = CP", Kind: ChartCreatedResolved, Days: MaxChartDays},
	}
	for _, in := range ok {
		if err := CheckChart(in); err != nil {
			t.Errorf("%+v was refused: %v", in, err)
		}
	}
	for field, in := range map[string]ChartInput{
		"project": {Project: "cp", Query: "project = CP", Kind: ChartPie, GroupBy: "type"},
		"q":       {Project: "CP", Query: " ", Kind: ChartPie, GroupBy: "type"},
		"groupBy": {Project: "CP", Query: "project = CP", Kind: ChartPie, GroupBy: "label"},
		"days":    {Project: "CP", Query: "project = CP", Kind: ChartCreatedResolved, Days: MaxChartDays + 1},
		"kind":    {Project: "CP", Query: "project = CP", Kind: "bar", GroupBy: "type"},
	} {
		var refused *FieldError
		if err := CheckChart(in); !errors.As(err, &refused) || refused.Field != field || refused.Message == "" {
			t.Errorf("%+v: %v, want a sentence on %s", in, err, field)
		}
	}
}

func TestEachChartIsCachedApart(t *testing.T) {
	token := uuid.New()
	base := ChartInput{Project: "CP", Query: "project = CP", Kind: ChartPie, GroupBy: "type", Days: DefaultChartDays}
	seen := map[string]bool{ChartField(token, base): true}
	for _, other := range []ChartInput{
		{Project: "SEC", Query: base.Query, Kind: base.Kind, GroupBy: base.GroupBy, Days: base.Days},
		{Project: base.Project, Query: "project = SEC", Kind: base.Kind, GroupBy: base.GroupBy, Days: base.Days},
		{Project: base.Project, Query: base.Query, Kind: base.Kind, GroupBy: "priority", Days: base.Days},
		{Project: base.Project, Query: base.Query, Kind: ChartCreatedResolved, GroupBy: base.GroupBy, Days: base.Days},
	} {
		field := ChartField(token, other)
		if seen[field] {
			t.Errorf("%+v shares a cache field with another chart", other)
		}
		seen[field] = true
	}
	if ChartField(uuid.New(), base) == ChartField(token, base) {
		t.Error("two people share a chart's cache field")
	}
}
