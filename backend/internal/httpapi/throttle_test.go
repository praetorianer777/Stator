package httpapi

import (
	"testing"
	"time"
)

func TestABrakeLetsTheLimitThroughPerKeyAndForgetsAfterTheWindow(t *testing.T) {
	brake := newThrottle(2, time.Minute)
	now := time.Now()
	if !brake.allow("a", now) || !brake.allow("a", now) {
		t.Fatal("the first two were refused")
	}
	if brake.allow("a", now) {
		t.Error("the third within the window went through")
	}
	if !brake.allow("b", now) {
		t.Error("another key was held by the first")
	}
	if !brake.allow("a", now.Add(time.Minute)) {
		t.Error("the key is still held once its window has passed")
	}
}

func TestABrakeForgetsQuietKeysBeforeItGrowsPastItsBound(t *testing.T) {
	brake := newThrottle(1, time.Minute)
	now := time.Now()
	for i := range throttleMaxKeys + 10 {
		brake.allow(string(rune(i)), now)
	}
	brake.allow("late", now.Add(time.Minute))
	if len(brake.seen) > throttleMaxKeys {
		t.Errorf("the brake remembers %d keys", len(brake.seen))
	}
}
