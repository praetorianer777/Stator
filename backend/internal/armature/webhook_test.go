package armature

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestSignMatchesArmature(t *testing.T) {
	// The HMAC as openssl dgst -sha256 -hmac works it out over the same bytes.
	body := []byte(`{"id":"0191d7c4-0000-7000-8000-000000000001","topic":"ping"}`)
	secret := "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	got := Sign(body, secret)
	if want := "sha256=8a68647645a52d1ecac79ad0976431d5edfd0b5b06caeba5a888efcaf40e0b0b"; got != want {
		t.Fatalf("Sign = %q, want %q", got, want)
	}
	if !VerifySignature(body, secret, got) {
		t.Error("a body does not verify against its own signature")
	}
	for name, header := range map[string]string{
		"another secret":    Sign(body, secret+"x"),
		"another body":      Sign(append(body, ' '), secret),
		"upper case hex":    "sha256=" + upper(got[7:]),
		"no prefix":         got[7:],
		"empty":             "",
		"another algorithm": "sha1=" + got[7:],
	} {
		if VerifySignature(body, secret, header) {
			t.Errorf("%s verifies", name)
		}
	}
}

func upper(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'f' {
			out[i] = c - 'a' + 'A'
		}
	}
	return string(out)
}

func TestWebhookClearsWhatEachTopicAnnounces(t *testing.T) {
	for _, tc := range []struct {
		topic, payload string
		keys           []string
		searches       bool
	}{
		{"issue.created", `{"key":"cp-9"}`, []string{"CP-9"}, true},
		{"issue.updated", `{"key":"CP-5","movedFrom":"SEC-2"}`, []string{"CP-5", "SEC-2"}, true},
		{"issue.updated", `{"key":"CP-1"}`, []string{"CP-1"}, true},
		{"issue.transitioned", `{"key":"CP-1"}`, []string{"CP-1"}, true},
		{"comment.added", `{"key":"CP-2"}`, []string{"CP-2"}, false},
		{"ping", `{}`, nil, false},
		{"worklog.added", `{"key":"CP-1"}`, nil, false},
		{"issue.updated", `{"key":"not a key"}`, nil, true},
		{"issue.updated", `not json`, nil, true},
	} {
		keys, searches := WebhookClears(tc.topic, json.RawMessage(tc.payload))
		if !slices.Equal(keys, tc.keys) || searches != tc.searches {
			t.Errorf("%s %s clears %v, searches %v; want %v, %v", tc.topic, tc.payload, keys, searches, tc.keys, tc.searches)
		}
	}
}
