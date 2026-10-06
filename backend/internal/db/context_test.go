package db

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestAReaderWithALinkIsAnonymousAndNamingSomebodyEndsIt(t *testing.T) {
	linked := WithLink(context.Background(), []byte{0xab, 0x01})
	if !AnonymousFrom(linked) || !LinkFrom(linked) {
		t.Fatal("a reader with a link is not an anonymous reader holding one")
	}
	if a, _ := linked.Value(userKey{}).(acting); a.link != "ab01" {
		t.Errorf("the link's digest is set as %q, want hex", a.link)
	}
	if LinkFrom(WithAnonymous(context.Background())) {
		t.Error("an anonymous reader without a link holds one")
	}
	if LinkFrom(WithUser(linked, uuid.New())) || AnonymousFrom(WithUser(linked, uuid.New())) {
		t.Error("naming a person kept the link")
	}
}

func TestATokensSpacesFollowItsPersonAndNoOther(t *testing.T) {
	person, other := uuid.New(), uuid.New()
	a, b := uuid.MustParse("019a0000-0000-7000-8000-00000000000a"), uuid.MustParse("019a0000-0000-7000-8000-00000000000b")

	limited := WithUserInSpaces(context.Background(), person, []uuid.UUID{a, b})
	if id, ok := UserFrom(limited); !ok || id != person {
		t.Fatalf("the limited context acts for %v %v, want %v", id, ok, person)
	}
	spaces, only := SpacesFrom(limited)
	if !only || len(spaces) != 2 {
		t.Fatalf("the limit is lost: %v %v", spaces, only)
	}
	if got := spacesSetting(spaces); got != "{"+a.String()+","+b.String()+"}" {
		t.Errorf("the setting reads %q", got)
	}
	// Somebody else named on the same context, as a worker does, is not
	// held to the token of the person before.
	if _, only := SpacesFrom(WithUser(limited, other)); only {
		t.Error("naming another person kept the token's limit")
	}
	if _, only := SpacesFrom(WithUser(context.Background(), person)); only {
		t.Error("a person without a token is limited")
	}
	// A token whose every space is gone reaches none, never all of them.
	none, only := SpacesFrom(WithUserInSpaces(context.Background(), person, nil))
	if !only || spacesSetting(none) != "{}" {
		t.Errorf("a token without spaces left = %v %q, want limited to none", only, spacesSetting(none))
	}
}

func TestAnAnonymousReaderIsNobodyAndNamingSomebodyEndsIt(t *testing.T) {
	anon := WithAnonymous(context.Background())
	if !AnonymousFrom(anon) {
		t.Fatal("the anonymous context is not anonymous")
	}
	if id, ok := UserFrom(anon); ok {
		t.Errorf("the anonymous context acts for %v", id)
	}
	if _, only := SpacesFrom(anon); only {
		t.Error("the anonymous context carries a token's limit")
	}
	if AnonymousFrom(WithUser(anon, uuid.New())) {
		t.Error("naming a person kept the context anonymous")
	}
	if AnonymousFrom(context.Background()) || AnonymousFrom(WithUser(context.Background(), uuid.New())) {
		t.Error("a context nobody made anonymous is")
	}
}
