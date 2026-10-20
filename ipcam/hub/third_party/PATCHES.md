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
- `require github.com/AndreiBesliu/DevWorld/ipcam/hub v0.0.0` (resolved by MTX-0's `replace`), `filippo.io/age` v1.3.2 (MTX-2's tests) and `go` 1.26.0 → 1.27.2, by `GOWORK=off go mod tidy` once MTX-2 imports `hub/internal/crypto`, then `v0.0.0` in place of the pseudo-version that tidy writes for a replaced module.
  - **Why.** MTX-2 encrypts with the hub's own age writer (C03, C04). The hub module declares `go 1.27.2`, so the fork, which requires it, must too. The versions it brings (`filippo.io/age` v1.3.2, `filippo.io/hpke` v0.4.0, `golang.org/x/crypto` v0.57.0) are the hub's.
  - **Files touched.** `go.mod`, `go.sum`.
  - **Test.** Every MTX-2 test imports `hub/internal/crypto`; the fork's govulncheck and license steps in `ipcam-spike-recording.yml` cover the new modules.
  - **Rebase status.** Redo with `GOWORK=off go mod tidy` after the re-vendor, and set the hub's requirement back to `v0.0.0`.

### MTX-0: own module path, no rpiCamera source, committed go:generate outputs

- **What.**
  1. **Module path.** The module is renamed from `github.com/bluenviron/mediamtx` to `github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx`, and every quoted import of the old path is rewritten. That also covers the package prefix that `internal/apidocsgen` matches on. URLs of the upstream project (the `upgrade` command, comments) are left alone. `go.mod` gets `replace github.com/AndreiBesliu/DevWorld/ipcam/hub => ../..`; the matching `require` came with MTX-2, the first patch that imports `hub/internal/crypto` (`go mod tidy` drops a requirement that nothing imports yet, and keeps the replace). The rewrite, as the script runs it:

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
  5. **Failures.** The hook runs only for a segment whose every write and fsync, `Close`, rename and directory sync succeeded; a part that failed before the close counts too, which the segment remembers (`partErr`). The error goes where upstream's errors go: the recorder logs it and restarts. The name left on disk depends on the step that failed:
     - a failed write, fsync, `Close` or rename leaves the `.part` name, for the hub's reconcile;
     - so does a failed directory sync after the `.part` is created, which leaves that `.part` empty;
     - a failed directory sync after a successful rename leaves the final name, without the hook: the hub's reconcile catalogs such a file (Task 5).
  6. **Windows.** The directory sync is a no-op (`dirsync_windows.go`): Windows has no directory fsync and NTFS journals the directory entries. The rename is `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH`, after `Close` (`rename_windows.go`). On other systems the directory is opened and fsynced (`dirsync_other.go`), and the rename is `os.Rename` (`rename_other.go`).
  7. **Seams.** `syncer.go` holds the three one-function seams that every call goes through, `syncFile`, `syncDir` and `renameFinal`, so that the tests can count and order the calls; on Windows, `moveFileEx` is a seam too. MTX-2 adds a fourth, `createFile`, and the `segmentFile` interface that `syncFile` takes.

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
  - `TestMTX1_SeamFailureFiresNoHook`: when the close's file fsync, the rename, the directory sync after the rename or the one after the creation fails, no hook runs; the `.part` name is left, except after a successful rename, which leaves the final name.
  - `TestMTX1_WindowsDirSyncNoOpAndWriteThroughRename` (`mtx1_windows_test.go`): the directory sync of a missing directory returns nil, so it opens no handle, and the rename passes `MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH` to `MoveFileEx` and replaces an existing file.

  The `recorder` job of `ipcam-spike-recording.yml` runs them on Linux with `-race`, the `windows` job on windows-latest.
- **Rebase status.** Applied on v1.21.1 (2026-10-20). Re-apply on each adopted MediaMTX release: the new files carry over as they are; in `format_fmp4_segment.go`, redo the `.part` creation, the directory sync after it, the fsync after each part and `partErr` in `closeCurPart`, and the close order in `close`; then adapt the create paths in `recorder_test.go` and run the tests above.

### MTX-2: age encryption at write

- **What.** A path with age recipients writes its fMP4 segments encrypted with age as they are recorded, so that no clear byte of a recording reaches the disk:
  1. **Configuration** (`internal/conf/path.go`, `age_recipients.go`, `jsonwrapper/unmarshal.go`).
     - `recordAgeRecipients: [<RI_g>, <RK>]`: exactly two different age1 X25519 recipients, checked when the configuration loads with `crypto.ParseKeyScope` (`Path.RecordAgeScope`). Any other value fails the load with an error that never quotes it: one or three entries, a secret key, a hybrid or SSH recipient, RK equal to RI_g, a value that is not a list, an empty list, null (also a YAML key with no value, as a block list whose entries were deleted leaves it), an empty environment variable, or an indexed one (`…_RECORDAGERECIPIENTS_0`). This holds in a path, in `pathDefaults`, from the environment and from the control API. The key needs `recordFormat: fmp4`, since MPEG-TS segments are not encrypted. Only without the key, in the path and in `pathDefaults`, does the path record in clear.
     - The field is `*AgeRecipients` (`internal/conf/age_recipients.go`), nil when the key is absent. A pointer, because `conf.Load` turns every nil slice into an empty one, so a `[]string` could not tell an absent key from an empty list. It holds the two strings, because `Path.Clone` copies settable fields only and would drop the key bytes inside a parsed recipient.
     - Null. MediaMTX reads null as an absent key: `jsonwrapper` sets a pointer to nil on null. `AgeRecipients` implements the new `jsonwrapper.NullRejecter`, so `jsonwrapper` refuses null for it (`cannot set "recordAgeRecipients" to null`); every other key keeps upstream's rule.
     - Environment. `AgeRecipients.UnmarshalEnv` reads one comma-separated variable. An empty variable is an empty list, refused as above; upstream's generic slice loader would panic on it for a nil pointer. A variable whose name only begins with the key's, as an indexed list would, fails the load.
     - `recordAgePadLowBitrate: true|false`, `true` when absent: the per-camera setting O17 („Pierdere minimă la pană de curent”).
     - `internal/core/path.go` passes them to the recorder as `Recorder.AgeScope`, a `*crypto.KeyScope` that holds public keys only, and `Recorder.AgePadLowBitrate`. A changed key recreates the path, since `pathConfCanBeUpdated` compares the whole path configuration.
  2. **Naming.** An encrypted segment is `<recordPath>.mp4.age`, written as `.mp4.age.part` and renamed by MTX-1.
  3. **Strictly in order.** `ageSegmentWriter` (`agewriter.go`) wraps the hub's `crypto.Writer`. It writes the age header for RI_g and RK when the file is created, then the init, each part's `moof` and `mdat`, the padding and the final chunk, all through the writer and in order. At most one chunk of plaintext stays in RAM.
  4. **Padding, per part** (§4.5 as amended by O17). Right after a part is written and before MTX-1's fsync:
     - `p` is the distance from the part's end in the plaintext to the next 64 KiB chunk boundary; when 0 < p < 8, too short for a free box's header, it is 65,536 bytes more;
     - the part is padded with a `free` box of `p` bytes when the setting is on and the part (its `moof` and `mdat`, without the init or the padding) is shorter than 64 KiB (`smallPartBytes`, equal to `crypto.ChunkSize`), or when `p` is at most 10 % of the part;
     - then `Flush` seals every complete chunk, so that the part's fsync makes everything up to the last chunk boundary durable.

     The decision is per part and keeps no state per stream. The spec's "stream below 512 kbps" is exactly a stream whose parts are under 64 KiB, about 524 kbps with 1 s parts, so the bitrate rule follows from the part size, and a stream whose bitrate varies (night VBR, smart codecs) is padded part by part.
  5. **No seek-back** (C13). The close does not seek back to write the `mvhd` duration of an encrypted segment: its `mvhd` keeps a duration of 0, and playback reads durations from the hub's catalog. The file is only ever appended to.
  6. **Close.** When every byte is sealed already (`Buffered() == 0`, as after a part padded to a boundary), an 8-byte `free` box is written first, so that the final chunk is never empty: age rejects an empty final chunk after other chunks. `Writer.Close` writes the final chunk; MTX-1's close order follows: one more `syncFile`, `Close`, `renameFinal`, `syncDir`, then MTX-3's hook. So the final chunk is durable before the final name.
  7. **Seam.** `createFile` (`syncer.go`) creates the segment file as a `segmentFile`, an interface that `*os.File` implements, so that a test can wrap the file; `syncFile` takes a `segmentFile`.
- **Why.** HUB-04 and C04: no clear frame on disk, with the hub's own age writer (C03). §4.5, R01 and O17 (ruling C1-R1): the `Flush` at every part's fsync bounds what a cut loses to the bytes after the last chunk boundary; padding the small parts keeps a low-bitrate stream within HUB-07 (median ≤ 1 s, maximum ≤ 2 s, with reconcile's whole-sample salvage, R01 and C1-R9) at the cost of disk for that camera only (about twice the bytes at 256 kbps); the 10 % rule aligns a large part when that is cheap. C13: the hub catalogs durations, so the file never needs a seek. C22: a secret key in the configuration is refused without being echoed. `TestMTX2_WorstCaseLoss` prints the loss and disk figures as the `mtx2-loss` notice of the recorder job.
- **Files touched.**
  - New: `internal/recorder/agewriter.go`, `internal/conf/age_recipients.go`.
  - `internal/recorder/format_fmp4_segment.go` (`close`, `closeCurPart`), `syncer.go` (`segmentFile`, `createFile`), `recorder.go` and `recorder_instance.go` (the two settings, at both places where an instance is built).
  - `internal/conf/path.go` (the keys, their default, `RecordAgeScope`, validation), `internal/conf/jsonwrapper/unmarshal.go` (`NullRejecter`), `internal/core/path.go` (`startRecording`).
  - Tests: `internal/recorder/mtx2_test.go`, `mtx2_loss_test.go`, `internal/conf/mtx2_test.go`, `internal/conf/jsonwrapper/mtx2_test.go`, `internal/core/mtx2_test.go`; `internal/recorder/mtx1_test.go` takes `segmentFile` in its seam wrappers.
  - Upstream test adapted: `internal/conf/conf_test.go`, where `TestConfFromFile`'s expected path has `RecordAgePadLowBitrate: true`.
- **Test.** The recorder tests run a real `Recorder` with MTX-1's seam wrappers and decrypt what it wrote:
  - `TestMTX2_EncryptedSegmentHasNoClearFrames` (HUB-04): neither the final file nor the `.part` at any fsync holds the `ftyp` and its brand, a `moof` followed by its `mfhd`, or the first 16 bytes of an SPS, PPS or IDR of the stream (whose NAL units are all at least 16 bytes), while the plaintext holds them; the file decrypts with `crypto.NewRecording(id).OpenReaderAt`, for RI_g and for RK.
  - `TestMTX2_PartBoundaryChunkAligned_WhenPaddingAtMost10Pct`: in a 4 Mbps stream every part follows the 10 % rule and every padded part ends on a chunk boundary; parts sized to need about 20,000 bytes are padded, parts that need about 60,000, over 10 %, are not.
  - `TestMTX2_LowBitrateDecidedPerPart`: with the setting on, a 63 KiB part is aligned and a 65 KiB part is not (HUB-07's boundary), while with it off neither is; in a 450 kbps ± 30 % stream exactly the parts under 64 KiB are aligned by the setting; a 4 Mbps stream follows the 10 % rule only.
  - `TestMTX2_PaddingTooShortForAFreeBoxTakesAChunkMore`: a part that ends 4 bytes before a boundary is padded with 65,540 bytes when the rules pad it.
  - `TestMTX2_FinalChunkSyncedBeforeRename`: the file at the last `syncFile` is the final file, complete for age, with or without the close's free box; `Close`, the rename, the directory sync and the hook follow.
  - `TestMTX2_FlushAtEachPartFsync`: at each part's fsync the file is `len(header) + 16 + n·65552` bytes, `n = ⌊plaintext ÷ 65536⌋`, and a prefix of the final file.
  - `TestMTX2_NoSeekBack`: an encrypted segment is written and closed, by the next keyframe and by the recorder's stop, through a file that refuses `Read`, `Seek`, `WriteAt` and `Truncate`; a clear segment, the control, does seek.
  - `TestMTX2_FinalChunkNeverEmpty`: a segment whose last part ends on a chunk boundary ends with an 8-byte `free` box in a final chunk of its own, and age decrypts it.
  - `TestMTX2_EncryptedNamingPartThenRename`: only `.mp4.age.part` exists while the segment is written and only `.mp4.age` after; the hooks get these names.
  - `TestMTX2_RestartedRecorderStillEncrypts`: after a failed fsync restarts the recorder, the next segment is encrypted too.
  - `TestMTX2_WorstCaseLoss` (R01, O17): the real part writer runs into a byte-counting sink over 10 minutes of 1 s parts at 20 fps, part sizes uniform within ± 30 %, with MediaMTX's close timing (a part is fsynced one frame after its end, a segment's last part at the next segment's keyframe). At 10,000 uniform cut times, the loss is the cut minus the PTS of the last sample wholly inside the plaintext sealed by the last fsync before it. With the padding on, the median is ≤ 1 s and the maximum ≤ 2 s at 4 Mbps, 450 kbps and 256 kbps, and the disk overhead at 4 Mbps is ≤ 10 %; with it off, the maximum at 256 kbps is ≤ 4 s (§4.5: 1 part + 64 KiB ÷ bitrate, rounded up to whole parts). It prints the figures for 4 Mbps, 512, 450, 384 and 256 kbps as one `mtx2-loss: <json>` line of at most 3,500 bytes once escaped, which the recorder job's `mtx2-loss` step turns into a notice.
  - `TestMTX2_RejectsSecretKeyInRecipients` (C22, `internal/conf`): a secret key as RI_g or RK, one or three recipients, a secret key instead of a list, RI_g as RK, an empty list (also with `mpegts`, and over recipients in `pathDefaults`), null and a key with no value each fail the load, in a path and in `pathDefaults`, with an error that contains no key.
  - `TestMTX2_RejectsEmptyRecipientsFromTheAPI`: a path that the control API adds with an empty list fails validation, and one with null fails to decode. `TestMTX2_RecipientsFromEnv`: one comma-separated variable sets the recipients of a path or of `pathDefaults`; an empty variable (also over recipients that the file sets), one recipient and an indexed variable fail the load. `TestMTX2_CloneKeepsRecipients`: `Conf.Clone` and `Path.Clone` copy the recipients into a list of their own.
  - `TestMTX2_RecipientsNeedFMP4`: the key with `recordFormat: mpegts` fails the load. `TestMTX2_NoRecipientsRecordsInClear`: only without the key, in either format, does the path load with no scope; a path without the key takes `pathDefaults`' recipients.
  - `TestMTX2_NullRejecterBehindPointer` (`internal/conf/jsonwrapper`): null for a `NullRejecter` behind a pointer fails with its key's name, nested too, and leaves the value as it was; a list and `[]` decode; other pointers still take null as nil.
  - `TestMTX2_LowBitratePadding_DefaultOn` and `TestMTX2_LowBitratePadding_Off` (`internal/core`): a MediaMTX core records a 256 kbps RTSP publisher into encrypted segments; with `recordAgePadLowBitrate` absent every part ends on a chunk boundary, with it `false` the parts follow the 10 % rule only.

  The `recorder` job of `ipcam-spike-recording.yml` runs them on Linux with `-race`, the `windows` job on windows-latest.
- **Rebase status.** Applied on v1.21.1 (2026-10-20). Re-apply on each adopted MediaMTX release: `agewriter.go` and `age_recipients.go` carry over as they are; redo the `NullRejecter` check in `jsonwrapper`'s `decode`, the two keys, their default and their validation in `conf/path.go`, the two settings in `recorder.go`, `recorder_instance.go` and `core/path.go`, the `.age` name, the age writer and `endPart` in `closeCurPart` and the age close in place of `writeDuration` in `close`; then adapt `TestConfFromFile` and run the tests above.

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
