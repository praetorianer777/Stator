package remotelink

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanTrimsWhatItKeeps(t *testing.T) {
	got, err := Clean(Input{URL: "  https://exciter.example/r/1 ", Title: " REQ-1 Sign in ", Source: " Exciter ", IconURL: " https://exciter.example/favicon-32.png "})
	if err != nil {
		t.Fatal(err)
	}
	want := Input{URL: "https://exciter.example/r/1", Title: "REQ-1 Sign in", Source: "Exciter", IconURL: "https://exciter.example/favicon-32.png"}
	if got != want {
		t.Errorf("Clean kept %+v, want %+v", got, want)
	}
	if _, err := Clean(Input{URL: "HTTP://exciter.example", Title: "T", Source: "S"}); err != nil {
		t.Errorf("an upper case scheme is refused: %v", err)
	}
	if _, err := Clean(Input{URL: "https://x.example", Title: strings.Repeat("é", MaxTitleLength), Source: strings.Repeat("s", MaxSourceLength)}); err != nil {
		t.Errorf("a title and source at their bounds are refused: %v", err)
	}
}

func TestCleanRefusesEachFieldInASentence(t *testing.T) {
	ok := Input{URL: "https://x.example/a", Title: "T", Source: "S"}
	with := func(change func(*Input)) Input {
		in := ok
		change(&in)
		return in
	}
	for what, c := range map[string]struct {
		in    Input
		field string
	}{
		"no url":            {with(func(in *Input) { in.URL = " " }), "url"},
		"another scheme":    {with(func(in *Input) { in.URL = "javascript:alert(1)" }), "url"},
		"ftp":               {with(func(in *Input) { in.URL = "ftp://x.example/a" }), "url"},
		"no host":           {with(func(in *Input) { in.URL = "https://" }), "url"},
		"a space inside":    {with(func(in *Input) { in.URL = "https://x.example/a b" }), "url"},
		"a relative url":    {with(func(in *Input) { in.URL = "/r/1" }), "url"},
		"too long a url":    {with(func(in *Input) { in.URL = "https://x.example/" + strings.Repeat("a", MaxURLLength) }), "url"},
		"an icon elsewhere": {with(func(in *Input) { in.IconURL = "data:image/png;base64,AAAA" }), "iconUrl"},
		"no title":          {with(func(in *Input) { in.Title = "  " }), "title"},
		"too long a title":  {with(func(in *Input) { in.Title = strings.Repeat("t", MaxTitleLength+1) }), "title"},
		"no source":         {with(func(in *Input) { in.Source = "" }), "source"},
		"too long a source": {with(func(in *Input) { in.Source = strings.Repeat("s", MaxSourceLength+1) }), "source"},
	} {
		_, err := Clean(c.in)
		var field *FieldError
		if !errors.As(err, &field) {
			t.Errorf("%s: %v, want a field error", what, err)
			continue
		}
		if field.Field != c.field {
			t.Errorf("%s is refused on %s, want %s", what, field.Field, c.field)
		}
		if !strings.HasSuffix(field.Message, ".") || strings.ToUpper(field.Message[:1]) != field.Message[:1] {
			t.Errorf("%s is refused with %q, which is no sentence", what, field.Message)
		}
	}
}
