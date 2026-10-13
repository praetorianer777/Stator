package spaceio

import (
	"fmt"

	"github.com/google/uuid"
)

// The folders the trigger job_object_key puts each transfer's archive in.
const (
	spaceImportFolder = "space-import"
	siteImportFolder  = "site-import"
)

// jobKey is the key the database gives a new transfer's archive, for an
// upload that has to come before the row. It must stay in step with
// job_object_key; whatever reads an archive later reads the row's key.
func jobKey(folder string, id uuid.UUID) string {
	return folder + "/" + id.String()
}

// sameKey refuses a row whose key is not where the bytes went, which only a
// trigger out of step with the helpers above would cause.
func sameKey(stored, uploaded string) error {
	if stored != uploaded {
		return fmt.Errorf("the database keeps the file at %s, not at %s where it went", stored, uploaded)
	}
	return nil
}
