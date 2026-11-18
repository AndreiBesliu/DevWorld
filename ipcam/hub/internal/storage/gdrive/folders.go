package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	folderMimeType = "application/vnd.google-apps.folder"
	// myDrive is the alias of My Drive's root folder, the root's parent.
	myDrive = "root"
)

// FolderStore keeps the Drive IDs of the driver's folders, by their path
// under My Drive ("ipcam", "ipcam/hub/cam01", …), so that a restart finds
// them with no files.list (P35, STO-07). The driver keeps the ID of each
// file that PutSmall wrote there too, by the file's path, so that a README
// or a catalog stays one file. The driver's uploads use it concurrently.
// catalog.DB implements it (Task 7b).
type FolderStore interface {
	Folder(ctx context.Context, destID, path string) (remoteID string, ok bool, err error)
	PutFolder(ctx context.Context, destID, path, remoteID string) error // inserts only if absent: the first stored ID wins (Task 7b)
}

// folder is one of the folders that hold a key: its path under My Drive,
// its name and its ID.
type folder struct{ path, name, id string }

// folders returns the folders of dir, a key's directory ("." for none):
// the root, then one per name of dir, each with its ID, creating those that
// the store does not hold (ensure).
func (d *Drive) folders(ctx context.Context, dir string, calls storage.CallCounts) ([]folder, error) {
	names := []string{d.rootName}
	if dir != "." {
		names = append(names, strings.Split(dir, "/")...)
	}
	chain := make([]folder, len(names))
	parent, p := myDrive, ""
	for i, name := range names {
		p = path.Join(p, name)
		id, err := d.ensure(ctx, p, name, parent, calls)
		if errors.Is(err, storage.ErrNotFound) && i > 0 {
			// The parent is gone: its owner deleted it (or a folder above it).
			d.markGone(chain[i-1].path)
			id, err = d.remake(ctx, chain[:i], p, name, calls)
		}
		if err != nil {
			return nil, err
		}
		chain[i] = folder{path: p, name: name, id: id}
		parent = id
	}
	return chain, nil
}

// ensure returns the ID of the folder at p, named name in parent. A folder
// that the store holds is used as it is. For another, ensure takes a create
// slot, gets an ID from files.generateIds and stores it before files.create
// (P35), so a crash between the two leaves an ID, which a later upload's
// 404 creates again (into). The store keeps the first ID stored for a path,
// so ensure reads it back and creates the ID that the store holds: another
// process's when that one stored first (g2-11). A 409 says that the folder
// exists, which is success too.
func (d *Drive) ensure(ctx context.Context, p, name, parent string, calls storage.CallCounts) (string, error) {
	if id, ok, err := d.stored(ctx, p); err != nil || ok {
		return id, err
	}
	if err := d.slot(methodCreate); err != nil {
		return "", err
	}
	id, err := d.storeNewID(ctx, p, calls)
	if err != nil {
		return "", err
	}
	if _, err := d.createFolder(ctx, id, name, parent, false, calls); err != nil {
		return "", err
	}
	return id, nil
}

// remake makes again the folders of chain, the parents of the folder at p
// whose create found its parent gone (recreate, which goes up as many levels
// as are gone), then creates that folder, named name, once more with the ID
// that ensure stored for p before its create (Task 5 review M1).
func (d *Drive) remake(ctx context.Context, chain []folder, p, name string, calls storage.CallCounts) (string, error) {
	id, ok, err := d.stored(ctx, p)
	switch {
	case err != nil:
		return "", err
	case !ok:
		return "", fmt.Errorf("gdrive: folder store: no ID for %s, whose create found its parent gone: %w", p, storage.ErrTransient)
	}
	if err := d.recreate(ctx, chain, len(chain)-1, calls); err != nil {
		return "", err
	}
	if _, err := d.createFolder(ctx, id, name, chain[len(chain)-1].id, true, calls); err != nil {
		return "", err
	}
	return id, nil
}

// into runs create with the ID of the folder that holds key, once the
// driver holds every folder of key's directory. A create that answers 404
// found that folder gone (its owner deleted it, or a crash came between the
// store and files.create): recreate makes it again with its ID, and create
// runs once more (P35).
func (d *Drive) into(ctx context.Context, key storage.ObjectKey, calls storage.CallCounts, create func(parent string) error) error {
	dir := path.Dir(string(key))
	if err := d.remakeGone(ctx, dir, calls); err != nil {
		return err
	}
	chain, err := d.folders(ctx, dir, calls)
	if err != nil {
		return err
	}
	parent := chain[len(chain)-1].id
	if err := create(parent); !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	d.markGone(chain[len(chain)-1].path)
	if err := d.recreate(ctx, chain, len(chain)-1, calls); err != nil {
		return err
	}
	return create(parent)
}

// remakeGone makes again, top-down and each with its stored ID, the
// folders of dir's chain that a create found gone (markGone), before the
// upload spends a create slot on anything else. Making them again
// bottom-up, as recreate does, spends a create slot on each create that
// finds its parent gone, so a chain gone two levels deep (a cut between
// files.generateIds and files.create, twice) took the second's two slots
// at every attempt, on the session's create and its day's, and the bucket
// denied the top folder's: every later attempt failed alike (P35, MI5).
// Top-down, each attempt makes the highest gone folder first and the
// chain converges. A folder whose create finds its own parent gone marks
// that parent, which then goes first.
func (d *Drive) remakeGone(ctx context.Context, dir string, calls storage.CallCounts) error {
	if !d.anyGone() {
		return nil
	}
	names := []string{d.rootName}
	if dir != "." {
		names = append(names, strings.Split(dir, "/")...)
	}
	parent, p, parentPath := myDrive, "", ""
	for _, name := range names {
		p = path.Join(p, name)
		id, ok, err := d.stored(ctx, p)
		if err != nil || !ok {
			return err // a folder that is not stored was never found gone
		}
		if d.isGone(p) {
			_, err := d.createFolder(ctx, id, name, parent, true, calls)
			if errors.Is(err, storage.ErrNotFound) && parentPath != "" && !d.isGone(parentPath) {
				d.markGone(parentPath)
				return d.remakeGone(ctx, dir, calls)
			}
			if err != nil {
				return err
			}
			d.clearGone(p)
		}
		parent, parentPath = id, p
	}
	return nil
}

// markGone records that the stored folder at p is gone; clearGone that it
// was made again.
func (d *Drive) markGone(p string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gone == nil {
		d.gone = map[string]bool{}
	}
	d.gone[p] = true
}

func (d *Drive) clearGone(p string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.gone, p)
}

func (d *Drive) isGone(p string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.gone[p]
}

func (d *Drive) anyGone() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.gone) > 0
}

// recreate creates again, with its stored ID, the folder chain[i] and as
// many of its parents as are gone: a create whose parent is gone answers
// 404 too, so recreate goes up a level and comes down again.
func (d *Drive) recreate(ctx context.Context, chain []folder, i int, calls storage.CallCounts) error {
	parent := myDrive
	if i > 0 {
		parent = chain[i-1].id
	}
	_, err := d.createFolder(ctx, chain[i].id, chain[i].name, parent, true, calls)
	if errors.Is(err, storage.ErrNotFound) && i > 0 {
		d.markGone(chain[i-1].path)
		if err = d.recreate(ctx, chain, i-1, calls); err == nil {
			_, err = d.createFolder(ctx, chain[i].id, chain[i].name, parent, true, calls)
		}
	}
	if err == nil {
		d.clearGone(chain[i].path)
	}
	return err
}

// createFolder creates the folder id named name in parent (files.create,
// taking a create slot when slot is true), and reports whether Drive created
// it now: a 409 says that a file with the ID exists, which is success too.
// A parent that is gone answers 404, ErrNotFound.
func (d *Drive) createFolder(ctx context.Context, id, name, parent string, slot bool, calls storage.CallCounts) (bool, error) {
	body, err := json.Marshal(map[string]any{"id": id, "name": name, "mimeType": folderMimeType, "parents": []string{parent}})
	if err != nil {
		return false, fmt.Errorf("gdrive: %s: %w: %w", methodCreate, storage.ErrPermanent, err)
	}
	a, err := d.send(ctx, call{
		method: methodCreate, verb: http.MethodPost, url: d.base + "/drive/v3/files?fields=id",
		header: map[string]string{"Content-Type": jsonType}, body: bytes.NewReader(body), length: int64(len(body)),
		create: slot, calls: calls,
	}, http.StatusOK, http.StatusConflict)
	if err != nil {
		return false, err
	}
	a.Close()
	return a.StatusCode == http.StatusOK, nil
}

// storeNewID gets an ID from files.generateIds, stores it for p and returns
// the ID that the store holds for p then: this one, or the one another
// process stored first.
func (d *Drive) storeNewID(ctx context.Context, p string, calls storage.CallCounts) (string, error) {
	a, err := d.send(ctx, call{
		method: methodGenerateIDs, verb: http.MethodGet,
		url: d.base + "/drive/v3/files/generateIds?count=1&space=drive&type=files", calls: calls,
	}, http.StatusOK)
	if err != nil {
		return "", err
	}
	var generated struct {
		IDs []string `json:"ids"`
	}
	if err := a.Decode(methodGenerateIDs, &generated); err != nil {
		return "", err
	}
	if len(generated.IDs) != 1 || generated.IDs[0] == "" {
		return "", fmt.Errorf("gdrive: %s: %d IDs, want 1: %w", methodGenerateIDs, len(generated.IDs), storage.ErrTransient)
	}
	if err := d.store.PutFolder(ctx, d.destID, p, generated.IDs[0]); err != nil {
		return "", fmt.Errorf("gdrive: folder store: %w", err)
	}
	id, ok, err := d.stored(ctx, p)
	if err == nil && !ok {
		err = errors.New("gdrive: folder store: no ID after PutFolder")
	}
	return id, err
}

// stored returns the ID that the store holds for p, if any.
func (d *Drive) stored(ctx context.Context, p string) (string, bool, error) {
	id, ok, err := d.store.Folder(ctx, d.destID, p)
	if err != nil {
		return "", false, fmt.Errorf("gdrive: folder store: %w", err)
	}
	return id, ok && id != "", nil
}
