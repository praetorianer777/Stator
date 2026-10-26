package main

import (
	"io"
	"strings"
	"testing"
)

// With no database or storage configured, reaching either would fail with
// the configuration's own error instead of the refusal.
func TestRefusalsComeBeforeAnythingIsOpened(t *testing.T) {
	t.Setenv("STATOR_DB_PRIMARY_URL", "")
	t.Setenv("STATOR_S3_ENDPOINT", "")
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"an unknown command":     {[]string{"restor", "-yes"}, `"restor" is not a backup command`},
		"a restore without -yes": {[]string{"restore", "-dir", t.TempDir()}, "Run it again with -yes"},
	} {
		err := run(c.args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: run = %v, want an error saying %q", name, err, c.want)
		}
	}
}
