package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestCatalog_Secrets (P19, §4.6): the sealed rows of the secrets table.
// PutSecret writes or replaces a row; SwapSecret replaces it only while it
// still holds the blob that the caller read, and otherwise changes nothing;
// SecretIDs lists the ids that start with a prefix, its characters taken
// as they are; a kind outside the table's CHECK is refused.
func TestCatalog_Secrets(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	if _, _, ok, err := db.Secret(ctx, "oauth:dst-1"); ok || err != nil {
		t.Fatalf("Secret of no row = %v, %v; want none", ok, err)
	}
	for _, id := range []string{"oauth:dst-1", "client:google:tv", "client:google:desktop", "oauth:dst-2", "clientXgoogle"} {
		if err := db.PutSecret(ctx, id, "oauth", []byte("blob-"+id), "software"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.PutSecret(ctx, "oauth:dst-1", "oauth", []byte("blob-2"), "software"); err != nil {
		t.Fatal(err)
	}
	if sealed, sealer, ok, err := db.Secret(ctx, "oauth:dst-1"); !ok || err != nil || string(sealed) != "blob-2" || sealer != "software" {
		t.Errorf("Secret = %q, %q, %v, %v; want the replaced blob", sealed, sealer, ok, err)
	}
	if ids, err := db.SecretIDs(ctx, "client:"); err != nil || !slices.Equal(ids, []string{"client:google:desktop", "client:google:tv"}) {
		t.Errorf("SecretIDs(client:) = %q, %v", ids, err)
	}
	if ids, err := db.SecretIDs(ctx, "oauth:"); err != nil || !slices.Equal(ids, []string{"oauth:dst-1", "oauth:dst-2"}) {
		t.Errorf("SecretIDs(oauth:) = %q, %v", ids, err)
	}
	if ids, err := db.SecretIDs(ctx, "client_"); err != nil || len(ids) != 0 {
		t.Errorf("SecretIDs(client_) = %q, %v; want none: the prefix is no LIKE pattern", ids, err)
	}
	if swapped, err := db.SwapSecret(ctx, "oauth:dst-1", []byte("blob-1"), []byte("blob-3"), "software"); swapped || err != nil {
		t.Errorf("SwapSecret from a stale blob = %v, %v; want no swap", swapped, err)
	}
	if swapped, err := db.SwapSecret(ctx, "oauth:dst-1", []byte("blob-2"), []byte("blob-3"), "software"); !swapped || err != nil {
		t.Errorf("SwapSecret from the stored blob = %v, %v; want a swap", swapped, err)
	}
	if sealed, _, _, _ := db.Secret(ctx, "oauth:dst-1"); string(sealed) != "blob-3" {
		t.Errorf("after the swap the row holds %q, want blob-3", sealed)
	}
	if swapped, err := db.SwapSecret(ctx, "oauth:dst-9", []byte("blob-2"), []byte("blob-3"), "software"); swapped || err != nil {
		t.Errorf("SwapSecret of no row = %v, %v; want no swap", swapped, err)
	}
	if err := db.DeleteSecret(ctx, "oauth:dst-2"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := db.Secret(ctx, "oauth:dst-2"); ok {
		t.Error("the deleted row is still there")
	}
	if err := db.PutSecret(ctx, "x", "bogus", []byte("b"), "software"); err == nil {
		t.Error("PutSecret of kind bogus succeeded, want the CHECK to refuse it")
	}
}

// writeCurrentCatalog makes a catalog of this hub's schema at path, with one
// secret row, and closes it, as a stopped hub leaves it.
func writeCurrentCatalog(t *testing.T, path string) {
	t.Helper()
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutSecret(t.Context(), "oauth:dst-1", "oauth", []byte("blob"), "software"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCatalog_OpenReadOnly (P45, C2-R15 (a)): OpenReadOnly reads an existing
// catalog of this hub's schema, never creates or migrates one, refuses any
// other schema version and every write, and reads while another connection
// holds the write lock, without waiting for it. A path with a space, a '#'
// or a '%' is a file name, not a part of a URI. (A '?' cuts Open's own
// plain DSN, so no catalog has one.)
func TestCatalog_OpenReadOnly(t *testing.T) {
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "a dir#x%41")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "catalog.db")
	if _, err := OpenReadOnly(ctx, path); err == nil {
		t.Fatal("OpenReadOnly of no catalog succeeded")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenReadOnly made %s (%v)", path, err)
	}
	writeCurrentCatalog(t, path)
	writer, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	// The writer holds the write lock, as the hub's transaction may.
	conn, err := writer.sql.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ro, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	if sealed, _, ok, err := ro.Secret(ctx, "oauth:dst-1"); !ok || err != nil || string(sealed) != "blob" {
		t.Errorf("Secret = %q, %v, %v", sealed, ok, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the read-only open and read took %v under the write lock, want no wait", took)
	}
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	if err := ro.PutSecret(ctx, "oauth:dst-2", "oauth", []byte("b"), "software"); err == nil {
		t.Error("a write through OpenReadOnly succeeded")
	}
	for _, version := range []int{len(migrations) - 1, len(migrations) + 1} {
		other := filepath.Join(t.TempDir(), "catalog.db")
		writeCurrentCatalog(t, other)
		setUserVersion(t, other, version)
		if _, err := OpenReadOnly(ctx, other); err == nil || !strings.Contains(err.Error(), "schema version") {
			t.Errorf("OpenReadOnly of a version-%d catalog = %v, want it refused", version, err)
		}
		if got := userVersion(t, other); got != version {
			t.Errorf("a refused open changed user_version from %d to %d", version, got)
		}
	}
}

// TestCatalog_OpenExisting: OpenExisting writes an existing catalog of this
// hub's schema, and creates, migrates and opens no other.
func TestCatalog_OpenExisting(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.db")
	if _, err := OpenExisting(ctx, path); err == nil {
		t.Fatal("OpenExisting of no catalog succeeded")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenExisting made %s (%v)", path, err)
	}
	writeCurrentCatalog(t, path)
	db, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if swapped, err := db.SwapSecret(ctx, "oauth:dst-1", []byte("blob"), []byte("blob-2"), "software"); !swapped || err != nil {
		t.Errorf("SwapSecret = %v, %v", swapped, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	setUserVersion(t, path, len(migrations)-1)
	if _, err := OpenExisting(ctx, path); err == nil || !strings.Contains(err.Error(), "schema version") {
		t.Errorf("OpenExisting of an older catalog = %v, want it refused", err)
	}
	if got := userVersion(t, path); got != len(migrations)-1 {
		t.Errorf("OpenExisting migrated the catalog to %d", got)
	}
}

// TestCatalog_OpenReadOnlyLeavesTheFilesAsTheyWere (C2-R15 (a)): what a
// read-only open of a WAL catalog leaves next to it. While the hub runs, its
// connection keeps -wal and -shm, and the read-only open creates no file.
// With the catalog closed, SQLite's read-only WAL open creates -wal and
// -shm when the directory lets it, and leaves them when it closes, since a
// read-only connection cannot checkpoint; on Linux, run as root, SQLite
// gives them the database file's owner (os_unix.c robustFchown), so root
// never leaves the hub a file that it cannot open.
func TestCatalog_OpenReadOnlyLeavesTheFilesAsTheyWere(t *testing.T) {
	ctx := t.Context()
	read := func(path string) {
		t.Helper()
		ro, err := OpenReadOnly(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := ro.Secret(ctx, "oauth:dst-1"); err != nil {
			t.Fatal(err)
		}
		if err := ro.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("while the hub runs", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "catalog.db")
		writeCurrentCatalog(t, path)
		hub, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = hub.Close() }()
		if _, _, _, err := hub.Secret(ctx, "oauth:dst-1"); err != nil {
			t.Fatal(err)
		}
		before := dirFiles(t, dir)
		read(path)
		if after := dirFiles(t, dir); !slices.Equal(after, before) {
			t.Errorf("files %q after the read-only open, want them as before: %q", after, before)
		}
	})
	t.Run("with the catalog closed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "catalog.db")
		writeCurrentCatalog(t, path)
		if got := dirFiles(t, dir); !slices.Equal(got, []string{"catalog.db"}) {
			t.Fatalf("a closed catalog leaves %q", got)
		}
		owner := -1
		if os.Geteuid() == 0 && runtime.GOOS != "windows" {
			owner = 65534 // nobody: the hub's own user stands apart from root
			for _, p := range []string{dir, path} {
				if err := os.Chown(p, owner, owner); err != nil {
					t.Fatal(err)
				}
			}
		}
		was, err := os.ReadFile(path) //nolint:gosec // the test's catalog
		if err != nil {
			t.Fatal(err)
		}
		read(path)
		files := dirFiles(t, dir)
		t.Logf("after a read-only open of a closed WAL catalog: %q", files)
		if now, err := os.ReadFile(path); err != nil || !bytes.Equal(now, was) { //nolint:gosec // the test's catalog
			t.Errorf("the read-only open changed catalog.db (%v)", err)
		}
		for _, f := range files {
			if f != "catalog.db" && f != "catalog.db-wal" && f != "catalog.db-shm" {
				t.Errorf("the read-only open left %s", f)
			}
			if owner < 0 {
				continue
			}
			info, err := os.Stat(filepath.Join(dir, f))
			if err != nil {
				t.Fatal(err)
			}
			if uid, ok := fileUID(info); ok && uid != owner {
				t.Errorf("%s is owned by %d, want the catalog's owner %d", f, uid, owner)
			}
		}
		// The hub opens the catalog again, with whatever the read left.
		hub, err := Open(ctx, path)
		if err != nil {
			t.Fatalf("the hub's open after the read: %v", err)
		}
		if err := hub.PutSecret(ctx, "oauth:dst-2", "oauth", []byte("b"), "software"); err != nil {
			t.Error(err)
		}
		_ = hub.Close()
	})
}

// dirFiles lists the names in dir, sorted.
func dirFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// setUserVersion sets the user_version of the catalog at path to version.
func setUserVersion(t *testing.T, path string, version int) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), "PRAGMA user_version = "+strconv.Itoa(version)); err != nil {
		t.Fatal(err)
	}
}

// userVersion is the user_version of the catalog at path.
func userVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var v int
	if err := db.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
