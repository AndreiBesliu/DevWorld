# Patches on third-party code

Every change that ipcam makes to vendored third-party code is listed here, one entry per patch, so that a rebase onto a new upstream release can redo each one and check it with its test. An entry says what changes, why, which files it touches, which test covers it, and its rebase status.

## MediaMTX (`mediamtx/`)

Upstream: `github.com/bluenviron/mediamtx` (MIT), vendored by `tools/scripts/vendor-mediamtx.sh`. The version, commit and checksums are in [`mediamtx/SOURCE.md`](mediamtx/SOURCE.md). The fork is a nested module outside `ipcam/go.work` (ruling C03), so every command below runs in `hub/third_party/mediamtx` with `GOWORK=off`:

- build: `GOWORK=off go build -o <bin>/mediamtx .`
- patch tests and the recorder's own tests: `GOWORK=off go test -run '^TestMTX' ./...` and `GOWORK=off go test ./internal/recorder/...`

The other upstream tests are not run. Some of them fail in the fork because of MTX-0 (see below).

**Rebasing.** `tools/scripts/vendor-mediamtx.sh <tag>` replaces the upstream tree (`main.go`, `internal/`, `LICENSE`, `go.mod`, `go.sum`) and applies MTX-0. It stops when one of MTX-0's anchors has moved. Then `GOWORK=off go mod tidy`, redo the dependency changes below that the new release still needs, update `SOURCE.md`, and redo MTX-1… from the diff of their commits, each with its test.

### Dependency versions that differ from upstream's go.mod

- `golang.org/x/net` v0.59.0 → v0.60.0, by `GOWORK=off go get golang.org/x/net@v0.60.0 && GOWORK=off go mod tidy`.
  - **Why.** The security exception to the 14-day rule (ruling R09). govulncheck finds GO-2026-6603, GO-2026-6610, GO-2026-6611, GO-2026-6612 and GO-2026-6617 (HTTP/2 in `net/http`) reachable from the fork, through the WHIP client and the packet dumper. All five are fixed in v0.60.0, which was published on 2026-10-08 and was 12 days old on 2026-10-20. Its requirements are versions the fork already uses.
  - **Files touched.** `go.mod`, `go.sum`.
  - **Test.** The fork's govulncheck step in `ipcam-spike-recording.yml`.
  - **Rebase status.** MediaMTX v1.21.2 still requires v0.59.0, so the re-vendor before Task 3 redoes this change. Drop it once upstream requires v0.60.0 or later.

### MTX-0: own module path, no rpiCamera source, committed go:generate outputs

- **What.**
  1. **Module path.** The module is renamed from `github.com/bluenviron/mediamtx` to `github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx`, and every quoted import of the old path is rewritten. That also covers the package prefix that `internal/apidocsgen` matches on. URLs of the upstream project (the `upgrade` command, comments) are left alone. `go.mod` gets `replace github.com/AndreiBesliu/DevWorld/ipcam/hub => ../..`; the matching `require` comes with MTX-2, the first patch that imports `hub/internal/crypto` (`go mod tidy` drops a requirement that nothing imports yet, and keeps the replace). The rewrite, as the script runs it:

     ```bash
     grep -rlZF --include='*.go' '"github.com/bluenviron/mediamtx/' . \
       | xargs -0 sed -i 's|"github.com/bluenviron/mediamtx/|"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/|g'
     go mod edit -module github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx \
       -replace github.com/AndreiBesliu/DevWorld/ipcam/hub=../..
     ```

     Imports are not re-sorted (no `gofmt`), so the rewrite stays a pure substitution.
  2. **No rpiCamera.** The Raspberry Pi camera source is removed:
     - its package `internal/staticsources/rpicamera` is deleted;
     - its registration is removed: the import and the `case s.Conf.Source == "rpiCamera":` of `internal/staticsources/handler.go`, which is where `internal/core`'s paths create their static sources;
     - the `rpiCamera` case of `internal/conf/path.go` now returns `source 'rpiCamera' is not supported: this build has no Raspberry Pi camera source` instead of validating the camera's settings, so a configuration that asks for the source is rejected when it loads, before a path could reach the handler's `panic("should not happen")`.

     The `rpiCamera*` fields of `conf.Path` stay, so a configuration that only sets them still parses. The constant `defs.APIPathSourceTypeRPICameraSource` and the field copies in `internal/core/path_manager.go` also stay; they are inert.
  3. **Generated files.** The module zip lacks the outputs of MediaMTX's `go:generate` steps, so they are committed:
     - `internal/servers/hls/hls.min.js`: `dist/hls.min.js` of the hls.js release that `internal/servers/hls/hlsjsdownloader/VERSION` names. The script downloads its `release.zip` and checks it against `hlsjsdownloader/HASH`, as upstream's downloader does.
     - `internal/core/VERSION`: the tag, without a line break, as upstream's `versiongetter` writes `git describe --tags`.

     The `go:generate` directives and their generator programs stay unchanged.
- **Why.**
  - C03: the fork needs a module path under `…/ipcam/hub/` to import `hub/internal/crypto` (MTX-2), and its own module keeps MediaMTX's ~100 requirements out of `hub/go.mod` and out of the workspace build list.
  - R05: the rpiCamera source embeds prebuilt `mtxrpicam` binaries that link LGPL libcamera into the Pi builds, where go-licenses cannot see them. The hub never uses a CSI camera.
  - C02: `go build` needs `hls.min.js` and `VERSION` for `go:embed`; committing them keeps the build offline and reproducible. Their SHA-256 and content are in `SOURCE.md`.
- **Files touched.**
  - `go.mod` (module line, replace).
  - Every `.go` file with an import of the old path (283 files on v1.21.1, substitution only).
  - `internal/staticsources/rpicamera/` (deleted).
  - `internal/staticsources/handler.go`, `internal/conf/path.go`.
  - `internal/servers/hls/hls.min.js`, `internal/core/VERSION` (added).
- **Test.** `mtx0_test.go`:
  - `TestMTX0_NoUpstreamImportPath`: no `.go` file imports the old path, whatever its build tags, and `go.mod` names the fork's path.
  - `TestMTX0_NoRpiCameraSource`: the package is gone, no file refers to it, and `conf.Load` rejects `source: rpiCamera` while it accepts the same file with an RTSP source.
  - `TestMTX0_GeneratedFilesMatchSourceMD`: the SHA-256 of `hls.min.js` and the content of `VERSION` are the ones `SOURCE.md` records, and `VERSION` names the vendored tag.

  Upstream tests that MTX-0 breaks, and that the fork does not run: the rpiCamera cases of `TestConfErrors` (`internal/conf`); `TestDefaultConfFile` and `internal/linters/conf`, which read `mediamtx.yml` from the upstream root, a file the fork does not vendor.
- **Rebase status.** Applied by `vendor-mediamtx.sh` on v1.21.1 (2026-10-20). Ruling C31 re-vendors v1.21.2 before Task 3, before any other patch exists.

### MTX-1: `.part` naming, fsync and rename

*Not applied yet: Task 3 writes this entry.*

### MTX-2: age encryption at write

*Not applied yet: Task 4 writes this entry.*

### MTX-3: segment-closed hook

*Not applied yet: Task 3 writes this entry.*
