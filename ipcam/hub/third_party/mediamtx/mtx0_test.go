package main

// The tests of patch MTX-0 (hub/third_party/PATCHES.md): the vendored MediaMTX
// tree has its own module path, carries no Raspberry Pi camera source, and its
// committed go:generate outputs are the ones SOURCE.md records.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf"
)

const (
	forkModulePath     = "github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx"
	upstreamModulePath = "github.com/bluenviron/mediamtx"
)

// TestMTX0_NoUpstreamImportPath: no .go file of the fork, whatever its build
// tags, imports a package by its upstream path, and go.mod declares the fork's
// own module path.
func TestMTX0_NoUpstreamImportPath(t *testing.T) {
	forkImports := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if imported == upstreamModulePath || strings.HasPrefix(imported, upstreamModulePath+"/") {
				t.Errorf("%s imports %s", path, imported)
			}
			if strings.HasPrefix(imported, forkModulePath+"/") {
				forkImports++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if forkImports == 0 {
		t.Errorf("no file imports a package of %s; the walk found nothing to check", forkModulePath)
	}

	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	module := ""
	for _, line := range strings.Split(string(goMod), "\n") {
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			module = strings.TrimSpace(rest)
			break
		}
	}
	if module != forkModulePath {
		t.Errorf("go.mod declares module %q, want %q", module, forkModulePath)
	}
}

// TestMTX0_NoRpiCameraSource: the rpiCamera static source, whose Pi binaries
// embed LGPL libcamera (R05), is gone: its package does not exist, no file
// refers to it, and a configuration that asks for it is rejected.
func TestMTX0_NoRpiCameraSource(t *testing.T) {
	// The package path, built from parts so that this file does not match it.
	pkg := "staticsources/" + "rpicamera"
	if _, err := os.Stat(filepath.Join("internal", filepath.FromSlash(pkg))); !os.IsNotExist(err) {
		t.Errorf("internal/%s still exists (stat: %v)", pkg, err)
	}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path) //nolint:gosec // path comes from walking the fork
		if err != nil {
			return err
		}
		if bytes.Contains(content, []byte(pkg)) {
			t.Errorf("%s refers to %s", path, pkg)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	load := func(source string) error {
		path := filepath.Join(t.TempDir(), "mediamtx.yml")
		yaml := "paths:\n  cam:\n    source: " + source + "\n"
		if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := conf.Load(path, nil, nil)
		return err
	}
	// The same configuration with another source loads, so the rejection
	// below is about the source and not about the file.
	if err := load("rtsp://127.0.0.1:8554/cam"); err != nil {
		t.Fatalf("a configuration with an RTSP source is rejected: %v", err)
	}
	if err := load("rpiCamera"); err == nil || !strings.Contains(err.Error(), "rpiCamera") {
		t.Errorf("a configuration with source rpiCamera gives %v, want an error that names rpiCamera", err)
	}
}

// TestMTX0_GeneratedFilesMatchSourceMD: the committed outputs of MediaMTX's
// go:generate steps are the ones SOURCE.md records, and VERSION names the
// vendored tag.
func TestMTX0_GeneratedFilesMatchSourceMD(t *testing.T) {
	doc, err := os.ReadFile("SOURCE.md")
	if err != nil {
		t.Fatal(err)
	}
	field := func(key string) string {
		t.Helper()
		match := regexp.MustCompile("(?m)^- " + regexp.QuoteMeta(key) + ": `([^`]+)`").FindSubmatch(doc)
		if match == nil {
			t.Fatalf("SOURCE.md has no line \"- %s: `…`\"", key)
		}
		return string(match[1])
	}

	hlsJS, err := os.ReadFile(filepath.Join("internal", "servers", "hls", "hls.min.js"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(hlsJS)
	if got, want := hex.EncodeToString(sum[:]), field("hls.min.js SHA-256"); got != want {
		t.Errorf("hls.min.js has SHA-256 %s, SOURCE.md records %s", got, want)
	}

	version, err := os.ReadFile(filepath.Join("internal", "core", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(version), field("VERSION"); got != want {
		t.Errorf("VERSION holds %q, SOURCE.md records %q", got, want)
	}
	if recorded, tag := field("VERSION"), field("Tag"); recorded != tag {
		t.Errorf("SOURCE.md records VERSION %q for tag %q", recorded, tag)
	}
}
