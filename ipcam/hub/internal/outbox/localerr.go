package outbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
)

// The local errors: those of the hub's own disk, its catalog and its spool
// files, which go to Observer.LocalError, each reported once while it lasts
// (g2-04), and never to stderr.

// localKind is where a local error comes from.
type localKind int

const (
	localCatalog localKind = iota
	localSpool
)

// localKey names what a local error is of: a spool file, by its path, or a
// catalog operation.
type localKey struct {
	kind localKind
	name string
}

// localErr is an error of the hub's own disk: of a catalog operation or of a
// spool file, which key names.
type localErr struct {
	key localKey
	err error
}

func (e *localErr) Error() string { return e.err.Error() }
func (e *localErr) Unwrap() error { return e.err }

// The catalog operations of the queue, by which their local errors are told
// apart.
const (
	opUploadingPlacements = "UploadingPlacements"
	opDuePlacements       = "DuePlacements"
	opLostPlacements      = "LostPlacements"
	opUpdatePlacement     = "UpdatePlacement"
	opSegmentByID         = "SegmentByID"
	opUploadSession       = "UploadSession"
	opPutUploadSession    = "PutUploadSession"
	opDeleteUploadSession = "DeleteUploadSession"
)

// spoolErr is a read error of the spool file at path, with the file's name
// and not its directory, as recorder's fileError writes them.
func spoolErr(path string, err error) *localErr {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return &localErr{key: localKey{kind: localSpool, name: path}, err: fmt.Errorf("outbox: read %s: %w", filepath.Base(path), err)}
}

// catalogErr takes err, what the catalog operation op returned. A success,
// or ErrStalePlacement, which the catalog returns as it should, ends op's
// local error and is returned as it is; any other error is returned as op's
// *localErr.
func (q *Queue) catalogErr(op string, err error) error {
	key := localKey{kind: localCatalog, name: op}
	if err == nil || errors.Is(err, catalog.ErrStalePlacement) {
		q.localOK(key)
		return err
	}
	return &localErr{key: key, err: err}
}

// catalogFailed reports err, what the catalog operation op returned, and
// whether op failed; ErrStalePlacement is no failure of the catalog.
func (q *Queue) catalogFailed(ctx context.Context, op string, err error) bool {
	var local *localErr
	if !errors.As(q.catalogErr(op, err), &local) {
		return false
	}
	q.localError(ctx, local)
	return true
}

// localError reports local, unless the error of its file or operation is
// already reported and that file or operation has not succeeded since, or
// ctx is done: a stop is no error (g2-02).
func (q *Queue) localError(ctx context.Context, local *localErr) {
	if ctx.Err() != nil {
		return
	}
	q.mu.Lock()
	reported := q.failing[local.key]
	q.failing[local.key] = true
	q.mu.Unlock()
	if !reported {
		q.opts.Observer.LocalError(local)
	}
}

// localOK ends the local error of key, a spool file that was read or a
// catalog operation that succeeded, so its next error is reported again.
func (q *Queue) localOK(key localKey) {
	q.mu.Lock()
	delete(q.failing, key)
	q.mu.Unlock()
}
