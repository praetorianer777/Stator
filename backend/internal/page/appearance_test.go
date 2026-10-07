package page

import "testing"

func TestAnIconIsOneEmoji(t *testing.T) {
	for _, ok := range []string{"🚀", "📘", "☕", "❤️", "👍🏽", "👩‍💻", "👨‍👩‍👧‍👦", "🇩🇪", "🏴‍☠️", "1️⃣", "#️⃣", "©️", "🏳️‍🌈"} {
		if !IsEmoji(ok) {
			t.Errorf("%q was refused", ok)
		}
	}
	for _, bad := range []string{"", "a", "Docs", "🚀 Docs", " 🚀", "12", "#", "😀1", "<b>", "‍", "️", "🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀", "\xff"} {
		if IsEmoji(bad) {
			t.Errorf("%q was admitted", bad)
		}
	}
}
