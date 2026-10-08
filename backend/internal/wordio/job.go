package wordio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// State is where an import of several documents stands.
type State string

const (
	StateQueued  State = "queued"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// States is every state, for the API's description.
var States = []State{StateQueued, StateRunning, StateDone, StateFailed}

// Open says the import is still to finish.
func (s State) Open() bool { return s == StateQueued || s == StateRunning }

// Failure is why an import stopped before its last document.
type Failure string

const (
	// FailureForbidden is a requester who may no longer add pages there.
	FailureForbidden Failure = "forbidden"
	// FailureParentGone is a parent page deleted or moved out of reach.
	FailureParentGone Failure = "parent_gone"
	// FailureFailed is anything else: an error, or an import that ran too long.
	FailureFailed Failure = "failed"
)

// Failures is every failure, for the API's description.
var Failures = []Failure{FailureForbidden, FailureParentGone, FailureFailed}

// Message is the failure in a sentence that says what to do about it.
func (f Failure) Message() string {
	switch f {
	case FailureForbidden:
		return "You may no longer add pages under that page, so the import stopped and the pages it made were moved to the trash. Ask an administrator of the space for the permission, then import again."
	case FailureParentGone:
		return "The page the documents were going under was deleted, so the import stopped and the pages it made were moved to the trash. Choose another page and import again."
	}
	return "The import failed, and the pages it made were moved to the trash. Import the documents again; if it fails once more, ask whoever runs Stator to look at the worker's log."
}

// FileReport is what became of one file of an import: the page it made with
// what did not come across, or why it made none.
type FileReport struct {
	Path string `json:"path"`
	// Page is the page made of it; null when it could not be read.
	Page *Made `json:"page"`
	// Warnings say what of the document did not come across as written.
	Warnings []string `json:"warnings"`
	// Error says why no page was made of it; null when one was.
	Error *string `json:"error"`
}

// Made is a page an import made, in reading order.
type Made struct {
	ID       uuid.UUID `json:"id"`
	ParentID uuid.UUID `json:"parentId"`
	Title    string    `json:"title"`
	// Depth is 1 for a page made right under the one imported into.
	Depth int `json:"depth"`
}

// Job is an import of several documents, as the page that queued it follows it.
type Job struct {
	ID       uuid.UUID `json:"id"`
	State    State     `json:"state"`
	ParentID uuid.UUID `json:"parentId"`
	// Done and Total count the pages made so far and to make, folders among them.
	Done  int `json:"done"`
	Total int `json:"total"`
	// Files is what became of each document and folder so far, in order.
	Files []FileReport `json:"files"`
	// Skipped names the files of the upload that are no Word documents.
	Skipped []string `json:"skipped"`
	// Failure says why the import stopped; null unless the state is failed.
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
SELECT id, state, parent_id, done_steps, total_steps, report, skipped, failure, requested_at, started_at, finished_at, COALESCE(written_lsn::text, '')
FROM word_import`

func scanJob(row pgx.Row) (*Job, error) {
	var (
		j       Job
		report  []byte
		written string
	)
	if err := row.Scan(&j.ID, &j.State, &j.ParentID, &j.Done, &j.Total, &report, &j.Skipped, &j.Failure, &j.RequestedAt, &j.StartedAt, &j.FinishedAt, &written); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(report, &j.Files); err != nil {
		return nil, fmt.Errorf("read an import's report: %w", err)
	}
	if j.Files == nil {
		j.Files = []FileReport{}
	}
	if j.Skipped == nil {
		j.Skipped = []string{}
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

// ErrJobNotFound is an import that is not there, or not the caller's.
var ErrJobNotFound = errors.New("word import not found")

// Queue stores an upload of several documents and asks the worker to make
// them pages under a page, as the caller.
func (s *Service) Queue(ctx context.Context, actor perm.Actor, parentID uuid.UUID, in []File) (*Job, db.LSN, error) {
	b, err := collect(in)
	if err != nil {
		return nil, 0, err
	}
	if n := count(tree(b.docs, "")); n > MaxPages {
		return nil, 0, invalid("the upload would make %d pages, more than the %d one import makes; import its folders one at a time", n, MaxPages)
	}
	if s.store == nil || objectstore.IsUnavailable(s.store) {
		return nil, 0, objectstore.ErrUnavailable
	}
	if _, err := s.checkParent(ctx, actor, parentID); err != nil {
		return nil, 0, err
	}
	packed, err := b.pack()
	if err != nil {
		return nil, 0, err
	}
	org, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, 0, errors.New("queue a Word import: no organization in the context")
	}
	id := uuid.Must(uuid.NewV7())
	key := ObjectKey(org.ID, id)
	if err := s.store.Put(ctx, key, bytes.NewReader(packed), int64(len(packed)), "application/zip"); err != nil {
		return nil, 0, fmt.Errorf("store the Word documents: %w", err)
	}
	var out *Job
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO word_import (id, org_id, requested_by, parent_id, file_count, size_bytes, skipped)
			VALUES ($1, current_org_id(), $2, $3, $4, $5, $6)`,
			id, actor.UserID, parentID, len(b.docs), b.size, nonNil(b.skipped)); err != nil {
			return err
		}
		var err error
		out, err = scanJob(tx.QueryRow(ctx, selectJob+` WHERE id = $1`, id))
		return err
	})
	if err != nil {
		_ = s.store.Delete(context.WithoutCancel(ctx), key)
		return nil, lsn, err
	}
	return out, lsn, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ObjectKey is where an import's upload waits for the worker.
func ObjectKey(org, id uuid.UUID) string {
	return objectstore.OrgPrefix(org) + "word-import/" + id.String()
}

// Job reads an import the caller queued.
func (s *Service) Job(ctx context.Context, actor perm.Actor, id uuid.UUID) (*Job, error) {
	var out *Job
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanJob(tx.QueryRow(ctx, selectJob+` WHERE id = $1 AND requested_by = $2`, id, actor.UserID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrJobNotFound
		}
		return err
	})
	return out, err
}
