package label

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func TestCheckReportNormalizesLabelsAndTidiesColumns(t *testing.T) {
	labels, columns, err := checkReport(ReportInput{Labels: []string{"Release Notes", "release-notes", "v2"}, Columns: []string{"  Owner ", "Review  date"}})
	if err != nil || strings.Join(labels, ",") != "release-notes,v2" || strings.Join(columns, ",") != "Owner,Review date" {
		t.Fatalf("%v %v %v", labels, columns, err)
	}
	for what, in := range map[string]ReportInput{
		"no label":         {},
		"too many labels":  {Labels: []string{"a", "b", "c", "d", "e", "f"}},
		"a label of words": {Labels: []string{"a/b"}},
		"a blank column":   {Labels: []string{"a"}, Columns: []string{" "}},
		"a long column":    {Labels: []string{"a"}, Columns: []string{strings.Repeat("x", document.MaxPropertyKeyLength+1)}},
		"too many columns": {Labels: []string{"a"}, Columns: strings.Split("a b c d e f g h i j k", " ")},
	} {
		var refused *FieldError
		if _, _, err := checkReport(in); !errors.As(err, &refused) || refused.Message == "" {
			t.Errorf("%s: %v", what, err)
		}
	}
}

func props(t *testing.T, rows ...string) []document.Property {
	t.Helper()
	var content []string
	for _, r := range rows {
		key, value, _ := strings.Cut(r, "=")
		content = append(content, `{"type":"propertyRow","attrs":{"key":"`+key+`"},"content":[{"type":"text","text":"`+value+`"}]}`)
	}
	var root document.Node
	if err := json.Unmarshal([]byte(`{"type":"doc","content":[{"type":"properties","content":[`+strings.Join(content, ",")+`]}]}`), &root); err != nil {
		t.Fatal(err)
	}
	return document.Properties(root)
}

func cells(r *PropertiesReport) string {
	var out []string
	for _, row := range r.Rows {
		var line []string
		for _, v := range row.Values {
			if v == nil {
				line = append(line, "-")
			} else {
				line = append(line, v.Text)
			}
		}
		out = append(out, row.Title+":"+strings.Join(line, "|"))
	}
	return strings.Join(out, " ")
}

func TestATableTakesEveryNameFoundOrTheColumnsAskedFor(t *testing.T) {
	pages := []reportPage{
		{row: ReportRow{Title: "Alpha"}, props: props(t, "Owner=Ada", "Status=Draft")},
		{row: ReportRow{Title: "Beta"}, props: props(t, "status=Final", "Review date=2026-11-01")},
		{row: ReportRow{Title: "Gamma"}},
	}
	all := tabulate(pages, nil, false)
	if strings.Join(all.Columns, ",") != "Owner,Status,Review date" || cells(all) != "Alpha:Ada|Draft|- Beta:-|Final|2026-11-01 Gamma:-|-|-" {
		t.Errorf("every name: %v %s", all.Columns, cells(all))
	}
	asked := tabulate(pages, []string{"STATUS", "Budget"}, true)
	if strings.Join(asked.Columns, ",") != "STATUS,Budget" || cells(asked) != "Alpha:Draft|- Beta:Final|- Gamma:-|-" || !asked.Truncated {
		t.Errorf("the columns asked for: %v %s", asked.Columns, cells(asked))
	}
	if none := tabulate(nil, nil, false); none.Columns == nil || none.Rows == nil {
		t.Error("an empty report answers empty lists, not null")
	}
}
