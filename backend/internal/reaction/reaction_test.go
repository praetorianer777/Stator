package reaction

import (
	"errors"
	"strings"
	"testing"
)

func TestCleanTakesOneEmoji(t *testing.T) {
	for _, emoji := range []string{
		"👍", "🎉", "❤️", "😄", "👀", "🚀", "✅",
		"👍🏽",      // a skin tone
		"👩‍💻",     // joined by a zero width joiner
		"👨‍👩‍👧‍👦", // a family, four joined
		"🇩🇪",      // a flag of two regional indicators
		"🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", // a subdivision flag, by tags
		"1️⃣", "#️⃣", // keycaps
		"©️",
	} {
		got, err := Clean(emoji)
		if err != nil || got != emoji {
			t.Errorf("Clean(%q) = %q, %v", emoji, got, err)
		}
	}
	if got, err := Clean("  🎉\n"); err != nil || got != "🎉" {
		t.Errorf("space around an emoji is not trimmed: %q, %v", got, err)
	}
}

func TestCleanRefusesWhatIsNotAnEmoji(t *testing.T) {
	for name, raw := range map[string]string{
		"nothing":            "",
		"space":              "   ",
		"a word":             "yes",
		"a word and emoji":   "ok👍",
		"a digit alone":      "1",
		"a digit and a tone": "1🏽",
		"a keycap alone":     "⃣",
		"a joiner first":     "‍👍",
		"a joiner last":      "👍‍",
		"a selector alone":   "️",
		"markup":             "<b>👍</b>",
		"two words":          "👍 👍",
		"too long":           strings.Repeat("👍", MaxEmojiBytes/4+1),
		"not utf-8":          "\xff",
	} {
		_, err := Clean(raw)
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "emoji" || !errors.Is(err, ErrNotEmoji) {
			t.Errorf("%s (%q) is not refused on emoji: %v", name, raw, err)
		}
	}
}
