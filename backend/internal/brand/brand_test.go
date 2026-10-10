package brand

import (
	"bytes"
	"errors"
	"testing"
)

func TestFooterPicksTheLanguageAndFallsBackToEnglish(t *testing.T) {
	both := Footer{En: "Internal", De: "Intern"}
	if got := both.In("de"); got != "Intern" {
		t.Errorf("German reads %q", got)
	}
	if got := both.In("de-AT"); got != "Intern" {
		t.Errorf("a regional German reads %q", got)
	}
	if got := both.In("en"); got != "Internal" {
		t.Errorf("English reads %q", got)
	}
	if got := (Footer{En: "Internal"}).In("de"); got != "Internal" {
		t.Errorf("a missing German line falls back to %q", got)
	}
}

func TestALogoIsWhatItsBytesSay(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	jpeg := append([]byte("\xff\xd8\xff\xe0"), make([]byte, 32)...)
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 32)...)
	for name, data := range map[string][]byte{"image/png": png, "image/jpeg": jpeg, "image/webp": webp} {
		got, err := sniffLogo(data)
		if err != nil || got != name {
			t.Errorf("%s was read as %q, %v", name, got, err)
		}
	}
	for name, data := range map[string][]byte{
		"an SVG":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		"text":     []byte("a logo"),
		"a GIF":    []byte("GIF89a" + string(make([]byte, 32))),
		"nothing":  nil,
		"a script": []byte("<script>alert(1)</script>"),
	} {
		if _, err := sniffLogo(data); !errors.Is(err, ErrBadLogoType) {
			t.Errorf("%s was taken as a logo: %v", name, err)
		}
	}
	if _, err := sniffLogo(append(png, bytes.Repeat([]byte{0}, MaxLogoBytes)...)); !errors.Is(err, ErrLogoTooLarge) {
		t.Errorf("an oversized logo gave %v", err)
	}
}
