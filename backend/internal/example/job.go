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

// Failure is why the example space was not made.
type Failure string

const (
	// FailureKeysTaken is every key the example tries taken by other spaces.
	FailureKeysTaken Failure = "keys_taken"
	// FailureForbidden is a requester who may no longer make the example.
	FailureForbidden Failure = "forbidden"
	// FailureFailed is anything else: an error, or a making that ran too long.
	FailureFailed Failure = "failed"
)

// Failures is every failure, for the API's description.
var Failures = []Failure{FailureKeysTaken, FailureForbidden, FailureFailed}

// Message is the failure in a sentence that says what to do about it.
func (f Failure) Message() string {
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
	State    State     `json:"state"`
	Language string    `json:"language"`
	// SpaceKey is the example's key once it is made; null before.
	SpaceKey *string `json:"spaceKey"`
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
SELECT j.id, j.state, j.language, s.key, j.failure, j.requested_at, j.started_at, j.finished_at, COALESCE(j.written_lsn::text, '')
FROM example_job j LEFT JOIN space s ON s.id = j.space_id AND j.state = 'done'`

func scanJob(row pgx.Row) (*Job, error) {
	var (
		j       Job
		written string
	)
	if err := row.Scan(&j.ID, &j.State, &j.Language, &j.SpaceKey, &j.Failure, &j.RequestedAt, &j.StartedAt, &j.FinishedAt, &written); err != nil {
		return nil, err
	}
	if j.Failure != nil {
		message := j.Failure.Message()
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
		out, err = scanJob(tx.QueryRow(ctx, selectJob+` WHERE j.org_id = current_org_id() ORDER BY j.requested_at DESC, j.id DESC LIMIT 1`))
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
		open := func() error {
			var err error
			out, err = scanJob(tx.QueryRow(ctx, selectJob+` WHERE j.org_id = current_org_id() AND j.state IN ('queued', 'running')`))
			if errors.Is(err, pgx.ErrNoRows) {
				out = nil
				return nil
			}
			return err
		}
		if err := open(); err != nil || out != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM space WHERE org_id = current_org_id() AND example)`).Scan(&exists); err != nil || exists {
			return err
		}
		// Two clicks at once both get here; the index lets one in, and the
		// other, having waited for it, reads the job it queued.
		var err error
		out, err = scanJob(tx.QueryRow(ctx, `
			WITH queued AS (
				INSERT INTO example_job (id, org_id, requested_by, language)
				VALUES ($1, current_org_id(), $2, $3)
				ON CONFLICT (org_id) WHERE state IN ('queued', 'running') DO NOTHING
				RETURNING *)
			SELECT j.id, j.state, j.language, NULL::text, j.failure, j.requested_at, j.started_at, j.finished_at, ''
			FROM queued j`, uuid.Must(uuid.NewV7()), actor.UserID, lang))
		if errors.Is(err, pgx.ErrNoRows) {
			return open()
		}
		return err
	})
	return out, lsn, err
}
