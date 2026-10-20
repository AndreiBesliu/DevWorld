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
  - **Rebase status.** MediaMTX v1.21.2 still requires v0.59.0, so a re-vendor to it redoes this change. Drop it once upstream requires v0.60.0 or later.

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
- **Rebase status.** Applied by `vendor-mediamtx.sh` on v1.21.1 (2026-10-20). Ruling C1-R8 keeps v1.21.1 for the spike, so MTX-1 and MTX-3 are built on it. The fork moves to v1.21.2 only if that release's changelog carries a fix to the recorder path or a security fix (checked before the release, in Task 13), by the rebase procedure above.

### MTX-1: `.part` naming, fsync and rename

- **What.** Upstream writes an fMP4 segment under its final name and never fsyncs it: there is no `.part` name, no rename and no fsync of the file or of its directory. MTX-1 introduces all of them, for fMP4 segments:
  1. **Naming.** A segment is created as `<recordPath>.mp4.part` and renamed to `<recordPath>.mp4` when it closes. `OnSegmentCreate` (and so `runOnRecordSegmentCreate`) gets the `.part` path, the file that exists while the segment is written. MTX-2 adds `.mp4.age.part` → `.mp4.age`.
  2. **Every part.** Right after a part is written, the file is fsynced, so each part is durable before the next part's first write.
  3. **Creation.** The directory is synced once, right after the `.part` is created.
  4. **Close, in this order.** The last write (for a clear segment, the duration in `mvhd`, written into the `.part` before the rename), one more file fsync, `Close`, the rename, the directory sync, and then MTX-3's hook. Without the last fsync, the directory sync could make the final name durable while the file's last bytes are still in the page cache (ext4's delayed allocation does not force them out on a rename to a new name).
  5. **Failures.** If a write, an fsync or `Close` of the segment fails, the segment keeps its `.part` name and no hook runs, and the error goes where upstream's errors go: the recorder logs it and restarts. The hub's reconcile then deals with the `.part`. That holds for a part that failed before the close too, which the segment remembers (`partErr`), and a failed rename or directory sync is handled the same way.
  6. **Windows.** The directory sync is a no-op (`dirsync_windows.go`): Windows has no directory fsync and NTFS journals the directory entries. The rename is `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH`, after `Close` (`rename_windows.go`). On other systems the directory is opened and fsynced (`dirsync_other.go`), and the rename is `os.Rename` (`rename_other.go`).
  7. **Seams.** `syncer.go` holds the three one-function seams that every call goes through, `syncFile`, `syncDir` and `renameFinal`, so that the tests can count and order the calls; on Windows, `moveFileEx` is a seam too.

  MPEG-TS segments are unchanged: ipcam records fMP4 only.
- **Why.** Rulings C13 and C14, spec §4.5: a part is durable when it closes, so a power cut loses at most the part being written (HUB-07), and the atomic rename means that a final name always points at complete, durable bytes, while an unfinished segment is recognisable by its name for the startup reconcile. The directory is synced at creation and at the rename only, not per part, to keep the fsyncs per open file down (§4.8).
- **Files touched.**
  - `internal/recorder/format_fmp4_segment.go` (`close`, `closeCurPart`).
  - New: `internal/recorder/syncer.go`, `dirsync_other.go`, `dirsync_windows.go`, `rename_other.go`, `rename_windows.go`.
  - Tests: `internal/recorder/mtx1_test.go`, `mtx1_windows_test.go`.
  - Upstream test adapted: `internal/recorder/recorder_test.go`, where the fMP4 create paths of `TestRecorder` and `TestRecorderFMP4SegmentSwitch` end in `.mp4.part`.
- **Test.** `mtx1_test.go` runs a real `Recorder` (1 s parts, 3 s segments) and wraps the seams, so every call still reaches the disk while the wrapper records what it saw:
  - `TestMTX1_FsyncOnEachPartClose`: a segment of 3 parts gets 4 file fsyncs. At each of the first three, the file ends exactly where that part ends; the fourth comes after the `mvhd` write, on the open file, and the file does not change after it.
  - `TestMTX1_DirSyncAtCreateAndRenameOnly` (not on Windows): exactly 2 directory syncs, the first when only the `.part` exists and before any part is written, the second right after the rename.
  - `TestMTX1_RenameAfterCloseWithFinalName`: while the segment is written only `<name>.mp4.part` exists, afterwards only `<name>.mp4`; the close runs file fsync (file open, `mvhd` written), rename (file closed), directory sync, hook.
  - `TestMTX1_ClearDurationWrittenBeforeRename`: `mvhd` holds the segment's duration in the `.part` at the rename and in the final file.
  - `TestMTX1_FailedPartSyncKeepsPartName`: when the second part's fsync fails, the segment is closed without a rename and without a hook, and only its `.part` is left.
  - `TestMTX1_WindowsDirSyncNoOpAndWriteThroughRename` (`mtx1_windows_test.go`): the directory sync of a missing directory returns nil, so it opens no handle, and the rename passes `MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH` to `MoveFileEx` and replaces an existing file.

  The `recorder` job of `ipcam-spike-recording.yml` runs them on Linux with `-race`, the `windows` job on windows-latest.
- **Rebase status.** Applied on v1.21.1 (2026-10-20). Re-apply on each adopted MediaMTX release: the new files carry over as they are; in `format_fmp4_segment.go`, redo the `.part` creation, the directory sync after it, the fsync after each part and `partErr` in `closeCurPart`, and the close order in `close`; then adapt the create paths in `recorder_test.go` and run the tests above.

### MTX-2: age encryption at write

*Not applied yet: Task 4 writes this entry.*

### MTX-3: segment-closed hook

- **What.**
  1. **The hook's start.** `recorder.OnSegmentCompleteFunc` becomes `func(path string, start time.Time, duration time.Duration)`. `start` is the segment's start: the wall time (NTP) of its first sample, the time that the file name encodes.
  2. **When it fires.** The fMP4 writer calls it once per segment, after MTX-1's rename and directory sync, with the final path. A segment whose close failed keeps its `.part` name and gets no call (MTX-1). The MPEG-TS writer passes its start too, because the type is shared; it fires as upstream's does.
  3. **The command's environment.** `internal/core/path.go` gives the `runOnRecordSegmentComplete` command `MTX_SEGMENT_START`, the start in Unix milliseconds, in decimal. It keeps upstream's variables: `MTX_PATH`, `MTX_SEGMENT_PATH` (now the final path), `MTX_SEGMENT_DURATION` (seconds, `strconv.FormatFloat(d.Seconds(), 'f', -1, 64)`), `RTSP_PATH`, `RTSP_PORT` and the `G1`… regexp groups. As upstream, `externalcmd` appends them to the environment of the MediaMTX process.
- **Why.** Ruling G05: the generated configuration sets `runOnRecordSegmentComplete` to `<ipcam-hub> hook segment-complete`, which forwards `{path, start, duration, …}` to the running `ipcam-hub`. Firing after the rename and the directory sync means that the catalog only learns of a segment whose final name is durable, and the start in milliseconds spares the hub from parsing it out of the file name.
- **Files touched.**
  - `internal/recorder/recorder.go` (the type and its default).
  - `internal/recorder/format_fmp4_segment.go` (the call, after MTX-1's rename and directory sync).
  - `internal/recorder/format_mpegts_segment.go` (passes its start).
  - `internal/core/path.go` (`MTX_SEGMENT_START`).
  - Tests: `internal/recorder/mtx3_test.go`, `internal/core/mtx3_test.go`.
  - Upstream test adapted: `internal/recorder/recorder_test.go`, where the `OnSegmentComplete` functions of `TestRecorder` and `TestRecorderTimeDriftDetector` take the start.
- **Test.**
  - `TestMTX3_HookFiresAfterRenameWithFinalPath`: for a segment closed by the next segment's keyframe and for one closed when the recorder stops, the order is rename, directory sync, hook; the hook gets the rename's target, which exists, and a start within 1 ms of the wall time of the segment's first frame.
  - `TestMTX3_HookEnvironment` (`internal/core`): a MediaMTX core records an RTSP publisher, and its hook command (the test binary, as `TestMTX3_HookHelperProcess`) reports what it sees: `MTX_PATH`; `MTX_SEGMENT_PATH`, the only file left in the directory, which exists while its `.part` no longer does; `MTX_SEGMENT_START`, decimal and equal to the start in the file name, in milliseconds; `MTX_SEGMENT_DURATION`, equal to the file's `mvhd`; and a variable set in the parent environment.
- **Rebase status.** Applied on v1.21.1 (2026-10-20). Re-apply on each adopted MediaMTX release: the type change, the two writers' calls and the environment variable in `path.go`; then adapt the `OnSegmentComplete` functions in `recorder_test.go` and run the tests above.
