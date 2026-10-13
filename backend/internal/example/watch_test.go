package example

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/config"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

func TestTheWatchIntervalIsTheConfiguredDefault(t *testing.T) {
	if DefaultWatchInterval != config.DefaultExampleCheck {
		t.Fatalf("example.DefaultWatchInterval %s and config.DefaultExampleCheck %s disagree", DefaultWatchInterval, config.DefaultExampleCheck)
	}
	if Lease <= RunLimit+CleanupLimit {
		t.Fatalf("a lease of %s lapses while a making of %s and its cleanup of %s may still run", Lease, RunLimit, CleanupLimit)
	}
}

func TestOnlyQueuedAndRunningAreOpen(t *testing.T) {
	for _, s := range States {
		if want := s == StateQueued || s == StateRunning; s.Open() != want {
			t.Errorf("%s open is %v", s, s.Open())
		}
	}
}

// Each failure is a sentence that says what to do, and an error is read as
// the failure it is.
func TestFailuresAreReadFromErrorsAndSaidInSentences(t *testing.T) {
	for _, f := range Failures {
		for _, k := range Kinds {
			m := f.Message(k)
			if !strings.HasSuffix(m, ".") || !strings.Contains(m, "example space") {
				t.Errorf("%s of %s says %q", f, k, m)
			}
		}
	}
	for err, want := range map[error]Failure{
		&space.FieldError{Field: "key", Message: "taken"}:               FailureKeysTaken,
		fmt.Errorf("make: %w", &space.FieldError{Field: "name"}):        FailureFailed,
		fmt.Errorf("make: %w", perm.ErrDenied):                          FailureForbidden,
		&perm.DeniedError{Action: perm.CreateExampleSpace}:              FailureForbidden,
		&pgconn.PgError{Code: pgerrInsufficientPrivilege}:               FailureForbidden,
		context.DeadlineExceeded:                                        FailureFailed,
		errors.Join(errors.New("lost the connection"), ErrNotDiscarded): FailureFailed,
		fmt.Errorf("renew: %w", ErrGone):                                FailureGone,
	} {
		if got := FailureOf(err); got != want {
			t.Errorf("%v is read as %s, want %s", err, got, want)
		}
	}
}

type fakeStore struct {
	claims    []*claim
	recorded  []uuid.UUID
	finished  []outcome
	discarded []uuid.UUID
	tracked   []Tracked
	lost      bool
	gone      bool
}

func (s *fakeStore) track(_ context.Context, _ *claim, _ uuid.UUID, t Tracked) error {
	s.tracked = append(s.tracked, t)
	return nil
}

func (s *fakeStore) edition(_ context.Context, id uuid.UUID) (*Edition, []Tracked, error) {
	if s.gone {
		return nil, nil, ErrGone
	}
	return &Edition{SpaceID: id, Version: 0, Language: English}, s.tracked, nil
}

func (s *fakeStore) claim(context.Context) (*claim, error) {
	if len(s.claims) == 0 {
		return nil, nil
	}
	c := s.claims[0]
	s.claims = s.claims[1:]
	return c, nil
}

func (s *fakeStore) started(_ context.Context, _ *claim, id uuid.UUID) error {
	if s.lost {
		return errLeaseLost
	}
	s.recorded = append(s.recorded, id)
	return nil
}

func (s *fakeStore) finish(_ context.Context, _ *claim, o outcome) error {
	s.finished = append(s.finished, o)
	return nil
}

func (s *fakeStore) discard(_ context.Context, id uuid.UUID) error {
	s.discarded = append(s.discarded, id)
	return nil
}

// fakeMaker makes a space, calls started, then fails with fail, as the
// Maker would, discarding its space unless discardFails.
type fakeMaker struct {
	store        *fakeStore
	fail         error
	beforeFail   func()
	discardFails bool
	made         []uuid.UUID
	discarded    []uuid.UUID
	renewals     []Renewal
	ctxs         []context.Context
}

func (m *fakeMaker) renew(ctx context.Context, _ *claim, in Renewal, wrote func(context.Context, Tracked) error) (*Report, db.LSN, error) {
	m.ctxs = append(m.ctxs, ctx)
	m.renewals = append(m.renewals, in)
	if err := wrote(ctx, Tracked{Name: Home, PageID: uuid.New(), Version: 2}); err != nil {
		return nil, 0, err
	}
	if m.fail != nil {
		return nil, 0, m.fail
	}
	return &Report{Added: []ReportPage{{ID: uuid.New(), Title: "New"}}}, 43, nil
}

func (m *fakeMaker) make(ctx context.Context, c *claim, started func(context.Context, *space.Space) error, wrote func(context.Context, Tracked) error) (*space.Space, db.LSN, error) {
	m.ctxs = append(m.ctxs, ctx)
	sp := &space.Space{ID: uuid.New(), Key: Key}
	m.made = append(m.made, sp.ID)
	err := started(ctx, sp)
	if err == nil {
		err = wrote(ctx, Tracked{Name: Home, PageID: sp.HomePageID, Version: 1})
	}
	if err == nil && m.beforeFail != nil {
		m.beforeFail()
	}
	if err == nil {
		err = m.fail
	}
	if err == nil {
		return sp, 42, nil
	}
	if cleanup := m.discard(ctx, c.actor(), sp.ID); cleanup != nil {
		err = errors.Join(err, fmt.Errorf("%w: %w", ErrNotDiscarded, cleanup))
	}
	return nil, 0, err
}

func (m *fakeMaker) discard(_ context.Context, _ perm.Actor, id uuid.UUID) error {
	if m.discardFails {
		return errors.New("the requester may not delete it")
	}
	m.discarded = append(m.discarded, id)
	return nil
}

func watchOf(store *fakeStore, maker *fakeMaker) *Watch {
	return &Watch{store: store, maker: maker, log: slog.New(slog.NewTextHandler(io.Discard, nil)), interval: DefaultWatchInterval}
}

func newClaim() *claim {
	return &claim{id: uuid.New(), requester: Person{ID: uuid.New(), Name: "Ada"}, role: auth.RoleAdmin, language: English, attempts: 1}
}

func TestAMakingThatSucceedsIsDoneWithItsSpace(t *testing.T) {
	store := &fakeStore{claims: []*claim{newClaim()}}
	maker := &fakeMaker{store: store}
	took, err := watchOf(store, maker).Once(context.Background())
	if !took || err != nil {
		t.Fatalf("took %v, %v", took, err)
	}
	if len(store.finished) != 1 || store.finished[0].state != StateDone || *store.finished[0].spaceID != maker.made[0] || store.finished[0].written != 42 {
		t.Fatalf("finished %+v", store.finished)
	}
	if len(store.recorded) != 1 || store.recorded[0] != maker.made[0] {
		t.Errorf("the space was recorded as %v, made %v", store.recorded, maker.made)
	}
	if took, err := watchOf(store, maker).Once(context.Background()); took || err != nil {
		t.Errorf("an empty queue was taken: %v, %v", took, err)
	}
}

func TestAMakingThatFailsLeavesNoSpaceAndSaysWhy(t *testing.T) {
	for name, tc := range map[string]struct {
		fail error
		want Failure
	}{
		"keys":   {&space.FieldError{Field: "key"}, FailureKeysTaken},
		"denied": {perm.ErrDenied, FailureForbidden},
		"else":   {errors.New("the database went away"), FailureFailed},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{claims: []*claim{newClaim()}}
			maker := &fakeMaker{store: store, fail: tc.fail}
			if _, err := watchOf(store, maker).Once(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(maker.discarded) != 1 || maker.discarded[0] != maker.made[0] {
				t.Errorf("discarded %v of %v", maker.discarded, maker.made)
			}
			if len(store.finished) != 1 || store.finished[0].state != StateFailed || store.finished[0].failure != tc.want || store.finished[0].spaceID != nil {
				t.Errorf("finished %+v", store.finished)
			}
		})
	}
}

// A requester who may no longer delete what was made leaves it to the
// worker, and the job fails all the same.
func TestASpaceTheRequesterCannotDeleteIsDeletedByTheWorker(t *testing.T) {
	store := &fakeStore{claims: []*claim{newClaim()}}
	maker := &fakeMaker{store: store, fail: perm.ErrDenied, discardFails: true}
	if _, err := watchOf(store, maker).Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.discarded) != 1 || store.discarded[0] != maker.made[0] {
		t.Errorf("the worker discarded %v of %v", store.discarded, maker.made)
	}
	if len(store.finished) != 1 || store.finished[0].failure != FailureForbidden {
		t.Errorf("finished %+v", store.finished)
	}
}

// A worker that stops part way deletes what it made, with the context it
// was stopped by ended, and hands the job back rather than failing it.
func TestAStoppingWorkerHandsTheJobBack(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	store := &fakeStore{claims: []*claim{newClaim()}}
	maker := &fakeMaker{store: store, beforeFail: stop, fail: context.Canceled}
	if _, err := watchOf(store, maker).Once(ctx); err != nil {
		t.Fatal(err)
	}
	if len(maker.discarded) != 1 {
		t.Errorf("discarded %v", maker.discarded)
	}
	if len(store.finished) != 1 || store.finished[0].state != StateQueued {
		t.Errorf("finished %+v", store.finished)
	}
}

// A job a dead worker left is taken over: its space goes first, then it is
// made again, until it has been tried too often.
func TestALapsedJobIsCleanedUpAndTriedAgain(t *testing.T) {
	left := uuid.New()
	again := newClaim()
	again.attempts, again.leftover = 2, &left
	tooOften := newClaim()
	tooOften.attempts, tooOften.leftover = MaxAttempts+1, &left
	store := &fakeStore{claims: []*claim{again, tooOften}}
	maker := &fakeMaker{store: store}
	w := watchOf(store, maker)
	for range 2 {
		if _, err := w.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(maker.discarded) != 2 || maker.discarded[0] != left || maker.discarded[1] != left {
		t.Errorf("discarded %v, want the leftover twice", maker.discarded)
	}
	if len(maker.made) != 1 {
		t.Errorf("made %d spaces, want one for the job still to be tried", len(maker.made))
	}
	if len(store.finished) != 2 || store.finished[0].state != StateDone || store.finished[1].state != StateFailed || store.finished[1].failure != FailureFailed {
		t.Errorf("finished %+v", store.finished)
	}
}

func TestARequesterWhoLeftIsRefused(t *testing.T) {
	gone := newClaim()
	gone.role = ""
	store := &fakeStore{claims: []*claim{gone}}
	maker := &fakeMaker{store: store}
	if _, err := watchOf(store, maker).Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(maker.made) != 0 || len(store.finished) != 1 || store.finished[0].failure != FailureForbidden {
		t.Errorf("made %v, finished %+v", maker.made, store.finished)
	}
}

// A worker whose job another took over stops, deletes what it made, and
// leaves the job to the other.
func TestAWorkerThatLostItsJobLeavesIt(t *testing.T) {
	store := &fakeStore{claims: []*claim{newClaim()}, lost: true}
	maker := &fakeMaker{store: store}
	if _, err := watchOf(store, maker).Once(context.Background()); !errors.Is(err, errLeaseLost) {
		t.Fatalf("a lost job answered %v", err)
	}
	if len(maker.discarded) != 1 || len(store.finished) != 0 {
		t.Errorf("discarded %v, finished %+v", maker.discarded, store.finished)
	}
}

// The making runs as the requester, bounded.
func TestTheMakingActsForTheRequester(t *testing.T) {
	c := newClaim()
	store := &fakeStore{claims: []*claim{c}}
	maker := &fakeMaker{store: store}
	if _, err := watchOf(store, maker).Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := maker.ctxs[0]
	if user, ok := db.UserFrom(ctx); !ok || user != c.requester.ID {
		t.Errorf("the making acts for %v", user)
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Error("the making has no limit")
	}
}

func renewClaim(target *uuid.UUID) *claim {
	c := newClaim()
	c.kind, c.target, c.language = KindRenew, target, German
	return c
}

// A renewal runs on the space the job names, keeps the pages it wrote, and
// ends with its report; it never makes or deletes a space.
func TestARenewalIsDoneWithItsReport(t *testing.T) {
	target := uuid.New()
	store := &fakeStore{claims: []*claim{renewClaim(&target)}}
	maker := &fakeMaker{store: store}
	if _, err := watchOf(store, maker).Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(maker.renewals) != 1 || maker.renewals[0].Space != target || maker.renewals[0].From != 0 || maker.renewals[0].Was != English || maker.renewals[0].Lang != German {
		t.Errorf("renewed %+v", maker.renewals)
	}
	if len(store.finished) != 1 || store.finished[0].state != StateDone || *store.finished[0].spaceID != target || store.finished[0].report == nil || store.finished[0].written != 43 {
		t.Fatalf("finished %+v", store.finished)
	}
	if len(store.tracked) != 1 || len(maker.made) != 0 || len(maker.discarded) != 0 || len(store.discarded) != 0 {
		t.Errorf("tracked %v, made %v, discarded %v and %v", store.tracked, maker.made, maker.discarded, store.discarded)
	}
}

func TestARenewalOfADeletedExampleSaysItIsGone(t *testing.T) {
	target := uuid.New()
	for name, tc := range map[string]struct {
		claim *claim
		store *fakeStore
	}{
		"deleted before it was claimed": {renewClaim(nil), &fakeStore{}},
		"deleted once it was claimed":   {renewClaim(&target), &fakeStore{gone: true}},
	} {
		t.Run(name, func(t *testing.T) {
			tc.store.claims = []*claim{tc.claim}
			maker := &fakeMaker{store: tc.store}
			if _, err := watchOf(tc.store, maker).Once(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(maker.renewals) != 0 || len(tc.store.finished) != 1 || tc.store.finished[0].failure != FailureGone {
				t.Errorf("renewed %v, finished %+v", maker.renewals, tc.store.finished)
			}
		})
	}
}

// A renewal that fails keeps what it renewed: nothing is deleted, and the
// job says why.
func TestARenewalThatFailsDeletesNothing(t *testing.T) {
	target := uuid.New()
	store := &fakeStore{claims: []*claim{renewClaim(&target)}}
	maker := &fakeMaker{store: store, fail: errors.New("the database went away")}
	if _, err := watchOf(store, maker).Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(maker.discarded) != 0 || len(store.discarded) != 0 {
		t.Errorf("discarded %v and %v", maker.discarded, store.discarded)
	}
	if len(store.finished) != 1 || store.finished[0].state != StateFailed || store.finished[0].failure != FailureFailed || store.finished[0].report != nil {
		t.Errorf("finished %+v", store.finished)
	}
}
