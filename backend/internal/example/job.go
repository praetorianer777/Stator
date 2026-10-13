package example

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/space"
)

// State is where the making of the example space stands.
type State string

const (
	StateQueued  State = "queued"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// States is every state, for the API's description.
var States = []State{StateQueued, StateRunning, StateDone, StateFailed}

// Open says the job is still to finish, so a second click finds it.
func (s State) Open() bool { return s == StateQueued || s == StateRunning }

// Kind is what a job does to the example space.
type Kind string

const (
	KindMake  Kind = "make"
	KindRenew Kind = "renew"
)

// Kinds is every kind, for the API's description.
var Kinds = []Kind{KindMake, KindRenew}

// Failure is why the example space was not made or renewed.
type Failure string

const (
	// FailureKeysTaken is every key the example tries taken by other spaces.
	FailureKeysTaken Failure = "keys_taken"
	// FailureForbidden is a requester who may no longer make the example.
	FailureForbidden Failure = "forbidden"
	// FailureFailed is anything else: an error, or a making that ran too long.
	FailureFailed Failure = "failed"
	// FailureGone is an example space deleted before it was renewed.
	FailureGone Failure = "gone"
)

// Failures is every failure, for the API's description.
var Failures = []Failure{FailureKeysTaken, FailureForbidden, FailureFailed, FailureGone}

// Message is the failure of a job of the kind in a sentence that says what
// to do about it.
func (f Failure) Message(kind Kind) string {
	if kind == KindRenew {
		switch f {
		case FailureForbidden:
			return "Only administrators of the organization renew the example space. Ask one of them to renew it."
		case FailureGone:
			return "The example space was deleted before it was renewed. Create the example space again to have the current one."
		}
		return "Renewing the example space failed part way. The pages it renewed stay renewed and the others as they were; renew it again, and if it fails once more, ask whoever runs Stator to look at the worker's log."
	}
	switch f {
	case FailureKeysTaken:
		return fmt.Sprintf("The keys %s to %s%d are all taken by other spaces. Delete or rename one of them, then create the example space again.", Key, Key, KeyTries)
	case FailureForbidden:
		return "Only administrators of the organization create the example space. Ask one of them to create it."
	}
	return "Creating the example space failed, and what was made of it was deleted. Create it again; if it fails once more, ask whoever runs Stator to look at the worker's log."
}

// FailureOf reads why a making failed from its error.
func FailureOf(err error) Failure {
	var (
		field  *space.FieldError
		denied *perm.DeniedError
		pgErr  *pgconn.PgError
	)
	switch {
	case errors.Is(err, ErrGone):
		return FailureGone
	case errors.As(err, &field) && field.Field == "key":
		return FailureKeysTaken
	case errors.As(err, &denied), errors.Is(err, perm.ErrDenied):
		return FailureForbidden
	case errors.As(err, &pgErr) && pgErr.Code == pgerrInsufficientPrivilege:
		return FailureForbidden
	}
	return FailureFailed
}

// pgerrInsufficientPrivilege is how the database refuses what a policy or a
// guard does not let the requester do.
const pgerrInsufficientPrivilege = "42501"

// Job is one making of the organization's example space, as the page that
// asked for it follows it.
type Job struct {
	ID       uuid.UUID `json:"id"`
	Kind     Kind      `json:"kind"`
	State    State     `json:"state"`
	Language string    `json:"language"`
	// SpaceKey is the example's key once it is made or renewed; null before.
	SpaceKey *string `json:"spaceKey"`
	// Report is what a renewal added and left to people; null otherwise.
	Report *Report `json:"report"`
	// Failure says why it was not made; null unless the state is failed.
	Failure *Failure `json:"failure"`
	// Message is the failure as a sentence that says what to do.
	Message     *string    `json:"message"`
	RequestedAt time.Time  `json:"requestedAt"`
	StartedAt   *time.Time `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	// Written is past the worker's last write, for the requester's next read.
	Written db.LSN `json:"-"`
}

const selectJob = `
SELECT j.id, j.kind, j.state, j.language, s.key, j.report, j.failure, j.requested_at, j.started_at, j.finished_at, COALESCE(j.written_lsn::text, '')
FROM example_job j LEFT JOIN space s ON s.id = j.space_id AND j.state = 'done'`

func scanJob(row pgx.Row) (*Job, error) {
	var (
		j       Job
		written string
	)
	if err := row.Scan(&j.ID, &j.Kind, &j.State, &j.Language, &j.SpaceKey, &j.Report, &j.Failure, &j.RequestedAt, &j.StartedAt, &j.FinishedAt, &written); err != nil {
		return nil, err
	}
	if j.Failure != nil {
		message := j.Failure.Message(j.Kind)
		j.Message = &message
	}
	if written != "" {
		lsn, err := db.ParseLSN(written)
		if err != nil {
			return nil, err
		}
		j.Written = lsn
	}
	return &j, nil
}

// Jobs queues the making of example spaces for the worker, and reads how it
// goes, as the administrator who asks.
type Jobs struct {
	db *db.Cluster
}

func NewJobs(cluster *db.Cluster) *Jobs {
	return &Jobs{db: cluster}
}

// Latest is the organization's latest making of its example, nil when it
// never asked for one.
func (j *Jobs) Latest(ctx context.Context, actor perm.Actor) (*Job, error) {
	var out *Job
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.CreateExampleSpace, uuid.Nil); err != nil {
			return err
		}
		var err error
		out, err = scanJob(tx.QueryRow(ctx, selectJob+` ORDER BY j.requested_at DESC, j.id DESC LIMIT 1`))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	return out, err
}

// Queue asks the worker to make the example in lang, or answers the making
// queued or running already. It answers nil when the example exists.
func (j *Jobs) Queue(ctx context.Context, actor perm.Actor, lang string) (*Job, db.LSN, error) {
	var out *Job
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.CreateExampleSpace, uuid.Nil); err != nil {
			return err
		}
		var err error
		if out, err = openJob(ctx, tx); err != nil || out != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM space WHERE example)`).Scan(&exists); err != nil || exists {
			return err
		}
		// Two clicks at once both get here; the index lets one in, and the
		// other, having waited for it, reads the job it queued.
		out, err = scanJob(tx.QueryRow(ctx, `
			WITH queued AS (
				INSERT INTO example_job (id, requested_by, language)
				VALUES ($1, $2, $3)
				ON CONFLICT (org_id) WHERE state IN ('queued', 'running') DO NOTHING
				RETURNING *)
			SELECT j.id, j.kind, j.state, j.language, NULL::text, NULL::jsonb, j.failure, j.requested_at, j.started_at, j.finished_at, ''
			FROM queued j`, uuid.Must(uuid.NewV7()), actor.UserID, lang))
		if errors.Is(err, pgx.ErrNoRows) {
			out, err = openJob(ctx, tx)
		}
		return err
	})
	return out, lsn, err
}

// openJob is the organization's job queued or running, nil when none is.
func openJob(ctx context.Context, tx db.DBTX) (*Job, error) {
	out, err := scanJob(tx.QueryRow(ctx, selectJob+` WHERE j.state IN ('queued', 'running')`))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// ErrNoExample refuses to renew an example space the organization does not have.
var ErrNoExample = errors.New("the organization has no example space to renew")

// Edition is the example space as a renewal sees it: the version of the
// content it was made or last renewed from, and the language of its pages.
type Edition struct {
	SpaceID  uuid.UUID
	Version  int
	Language string
}

// Edition is the organization's example as those who may renew it read it,
// nil when there is none.
func (j *Jobs) Edition(ctx context.Context, actor perm.Actor) (*Edition, error) {
	var out *Edition
	err := j.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.CreateExampleSpace, uuid.Nil); err != nil {
			return err
		}
		var err error
		out, err = editionOf(ctx, tx)
		return err
	})
	return out, err
}

func editionOf(ctx context.Context, tx db.DBTX) (*Edition, error) {
	var e Edition
	err := tx.QueryRow(ctx, `
		SELECT id, example_version, example_language FROM space
		WHERE example`).Scan(&e.SpaceID, &e.Version, &e.Language)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// Renew asks the worker to renew the example in lang, empty for the language
// its pages are in, or answers the job queued or running already.
func (j *Jobs) Renew(ctx context.Context, actor perm.Actor, lang string) (*Job, db.LSN, error) {
	var out *Job
	lsn, err := j.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := perm.Check(ctx, tx, actor, perm.CreateExampleSpace, uuid.Nil); err != nil {
			return err
		}
		var err error
		if out, err = openJob(ctx, tx); err != nil || out != nil {
			return err
		}
		e, err := editionOf(ctx, tx)
		if err != nil {
			return err
		}
		if e == nil {
			return ErrNoExample
		}
		if lang == "" {
			lang = e.Language
		}
		out, err = scanJob(tx.QueryRow(ctx, `
			WITH queued AS (
				INSERT INTO example_job (id, requested_by, language, kind, space_id)
				VALUES ($1, $2, $3, 'renew', $4)
				ON CONFLICT (org_id) WHERE state IN ('queued', 'running') DO NOTHING
				RETURNING *)
			SELECT j.id, j.kind, j.state, j.language, NULL::text, NULL::jsonb, j.failure, j.requested_at, j.started_at, j.finished_at, ''
			FROM queued j`, uuid.Must(uuid.NewV7()), actor.UserID, lang, e.SpaceID))
		if errors.Is(err, pgx.ErrNoRows) {
			out, err = openJob(ctx, tx)
		}
		return err
	})
	return out, lsn, err
}
