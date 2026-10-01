package document

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func inlinePara(inner string) string {
	return `{"type":"doc","content":[{"type":"paragraph","content":[` + inner + `]}]}`
}

func status(label, color string) string {
	l, _ := json.Marshal(label)
	return `{"type":"status","attrs":{"label":` + string(l) + `,"color":"` + color + `"}}`
}

func date(day string) string {
	return `{"type":"date","attrs":{"date":"` + day + `"}}`
}

func TestStatusesAndDatesAreAccepted(t *testing.T) {
	body := `{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"Release "},` + status("In review", "warning") + `,{"type":"text","text":" by "},` + date("2026-10-15") + `]},
		{"type":"heading","attrs":{"level":1,"id":"due"},"content":[` + date("2028-02-29") + `]},
		{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[` + status("Done", "success") + `]}]}]}]}
	]}`
	if err := Validate(json.RawMessage(body)); err != nil {
		t.Fatalf("a page with statuses and dates was refused: %v", err)
	}
	for _, color := range StatusColors {
		if err := Validate(json.RawMessage(inlinePara(status("x", color)))); err != nil {
			t.Errorf("the colour %q was refused: %v", color, err)
		}
	}
	if err := Validate(json.RawMessage(inlinePara(status(strings.Repeat("é", MaxStatusLength), "neutral")))); err != nil {
		t.Errorf("a label of %d characters was refused: %v", MaxStatusLength, err)
	}
}

func TestStatusesAndDatesAreRefusedInASentence(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"status without a label", inlinePara(status("   ", "neutral")), `label="   "`},
		{"status label too long", inlinePara(status(strings.Repeat("a", MaxStatusLength+1), "neutral")), `label=`},
		{"status colour as hex", inlinePara(status("Done", "#00ff00")), `color="#00ff00"`},
		{"status colour by name", inlinePara(status("Done", "green")), `color="green"`},
		{"status without a colour", inlinePara(`{"type":"status","attrs":{"label":"Done","color":null}}`), `color=null`},
		{"status with a style", inlinePara(`{"type":"status","attrs":{"label":"Done","color":"success","style":"x"}}`), `attribute "style"`},
		{"status at the top", `{"type":"doc","content":[` + status("Done", "success") + `]}`, `puts a "status"`},
		{"status with text inside", inlinePara(`{"type":"status","attrs":{"label":"Done","color":"success"},"content":[{"type":"text","text":"x"}]}`), `holds none`},
		{"date with a time", inlinePara(date("2026-10-15T10:00:00Z")), `date=`},
		{"date in another order", inlinePara(date("15.10.2026")), `date=`},
		{"date of a month that is not", inlinePara(date("2026-13-01")), `date="2026-13-01"`},
		{"date of a day that is not", inlinePara(date("2026-02-30")), `date="2026-02-30"`},
		{"date of a leap day in a common year", inlinePara(date("2027-02-29")), `date="2027-02-29"`},
		{"date as a number", inlinePara(`{"type":"date","attrs":{"date":20261015}}`), `date=20261015`},
		{"date empty", inlinePara(`{"type":"date","attrs":{"date":null}}`), `date=null`},
		{"date at the top", `{"type":"doc","content":[` + date("2026-10-15") + `]}`, `puts a "date"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(json.RawMessage(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%v does not wrap ErrInvalid", err)
			}
		})
	}
}

func TestACommentHoldsNoStatusOrDate(t *testing.T) {
	for _, node := range []string{status("Done", "success"), date("2026-10-15")} {
		if _, err := ParseComment(json.RawMessage(inlinePara(node))); err == nil {
			t.Errorf("a comment took %s", node)
		}
	}
}

func TestPlainTextReadsStatusesAndDates(t *testing.T) {
	root, err := Parse(json.RawMessage(`{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"Launch "},` + status("Blocked", "danger") + `,{"type":"text","text":" until "},` + date("2026-11-02") + `]},
		{"type":"table","content":[{"type":"tableRow","content":[
			{"type":"tableCell","content":[{"type":"paragraph","content":[` + status("Done", "success") + `]}]},
			{"type":"tableCell","content":[{"type":"paragraph","content":[` + date("2026-01-31") + `]}]}]}]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "Launch Blocked until 2026-11-02\nDone\t2026-01-31"
	if got := PlainText(root); got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}
