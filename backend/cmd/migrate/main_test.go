package main

import (
	"strings"
	"testing"
	"time"
)

// The database named here never answers, so any wait for it would take
// connectAttempts seconds before the refusal.
func TestCommandsAreRefusedWithoutWaitingForTheDatabase(t *testing.T) {
	t.Setenv("STATOR_DB_PRIMARY_URL", "postgres://stator@127.0.0.1:1/stator?connect_timeout=1")
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"an unknown command":   {[]string{"upp"}, `"upp" is not a migrate command`},
		"up-to with no number": {[]string{"up-to"}, "up-to needs a version"},
		"up-to with a word":    {[]string{"up-to", "latest"}, `"latest" is not a migration version`},
	} {
		start := time.Now()
		err := run(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: run = %v, want an error saying %q", name, err, c.want)
		}
		if took := time.Since(start); took > connectPause {
			t.Errorf("%s: refused after %s, so it waited for the database", name, took)
		}
	}
}
