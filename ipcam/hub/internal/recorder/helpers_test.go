package recorder

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// The test binary also plays the processes that a recorder starts:
//   - with fakeChildEnv set, it is a fake MediaMTX (runFakeChild);
//   - with the arguments "hook segment-complete", it is ipcam-hub's hook
//     command, as MediaMTX runs it;
//   - with helperEnv set, it is a process that runs New and waits to be
//     killed (TestRecorder_ChildDiesWithParent).
const (
	fakeChildEnv  = "IPCAM_RECORDER_FAKE_CHILD"
	helperEnv     = "IPCAM_RECORDER_HELPER"
	helperBinEnv  = "IPCAM_RECORDER_HELPER_MEDIAMTX" // the helper runs this MediaMTX instead of a fake
	helperJobsEnv = "IPCAM_RECORDER_HELPER_JOBS"     // and applies these jobs (helperJob, as JSON)
)

// helperJob is a job that the helper process applies.
type helperJob struct {
	Cam, Profile, URL string
	Recipients        []string // RI_g and RK, or none for a clear job
}

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeChildEnv); mode != "" {
		os.Exit(runFakeChild(mode, os.Args[1:]))
	}
	if len(os.Args) == 3 && os.Args[1] == "hook" && os.Args[2] == "segment-complete" {
		if err := PostSegmentComplete(context.Background(), os.Getenv); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if dir := os.Getenv(helperEnv); dir != "" {
		os.Exit(runHelper(dir))
	}
	os.Exit(m.Run())
}

// fakeChildDump is what a fake MediaMTX in "dump:<file>" mode writes.
type fakeChildDump struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
	PID  int      `json:"pid"`
}

// runFakeChild acts as MediaMTX started with args, in one of these modes:
//   - "sleep": it waits to be killed;
//   - "dump:<file>": it writes its arguments, environment and PID to file,
//     then waits;
//   - "stderr:<n>": it writes the lines "line 1" … "line <n>" to stderr,
//     then waits.
func runFakeChild(mode string, args []string) int {
	switch {
	case mode == "sleep":
	case strings.HasPrefix(mode, "dump:"):
		b, _ := json.Marshal(fakeChildDump{Args: args, Env: os.Environ(), PID: os.Getpid()})
		tmp := strings.TrimPrefix(mode, "dump:") + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil { //nolint:gosec // a file that the test named
			return 2
		}
		if err := os.Rename(tmp, strings.TrimPrefix(mode, "dump:")); err != nil { //nolint:gosec // a file that the test named
			return 2
		}
	case strings.HasPrefix(mode, "stderr:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(mode, "stderr:"))
		for i := 1; i <= n; i++ {
			fmt.Fprintf(os.Stderr, "line %d\n", i)
		}
	default:
		return 2
	}
	time.Sleep(time.Hour)
	return 0
}

// runHelper runs New in dir and waits until it is killed. Its child is a
// fake that dumps its PID to dir/child.json, or the MediaMTX in
// helperBinEnv, which records the jobs in helperJobsEnv.
func runHelper(dir string) int {
	self, _ := os.Executable()
	bin := os.Getenv(helperBinEnv)
	if bin == "" {
		bin = self
		if err := os.Setenv(fakeChildEnv, "dump:"+filepath.Join(dir, "child.json")); err != nil {
			return 2
		}
	}
	var jobs []Job
	if js := os.Getenv(helperJobsEnv); js != "" {
		var defs []helperJob
		if err := json.Unmarshal([]byte(js), &defs); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		for _, d := range defs {
			var scope *crypto.KeyScope
			if len(d.Recipients) == 2 {
				s, err := crypto.ParseKeyScope(d.Recipients[0], d.Recipients[1])
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				scope = &s
			}
			jobs = append(jobs, NewJob(CameraID(d.Cam), Profile(d.Profile), d.URL, scope))
		}
	}
	ctx := context.Background()
	cat, err := catalog.Open(ctx, filepath.Join(dir, "catalog.db"))
	if err != nil {
		return 2
	}
	r, _, err := New(ctx, Config{BinaryPath: bin, RunDir: filepath.Join(dir, "run"), RecDir: filepath.Join(dir, "rec"), Catalog: cat})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if jobs != nil {
		if err := r.Apply(ctx, jobs); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	// For the test that kills this process, to wait until MediaMTX died too.
	pid := []byte(strconv.Itoa(r.(*rec).childPID()))
	if err := os.WriteFile(filepath.Join(dir, "mediamtx.pid"), pid, 0o600); err != nil { //nolint:gosec // a file in the test's directory
		return 2
	}
	time.Sleep(time.Hour)
	return 0
}

// waitFor polls cond every 20 ms until it holds or d has passed.
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("after %v: %s", d, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readDump waits for the dump of a fake child.
func readDump(t *testing.T, path string) fakeChildDump {
	t.Helper()
	var d fakeChildDump
	waitFor(t, 10*time.Second, "the fake child wrote no dump", func() bool {
		b, err := os.ReadFile(path) //nolint:gosec // path is in the test's directory
		return err == nil && json.Unmarshal(b, &d) == nil
	})
	return d
}

func newTestScope(t *testing.T) (scope crypto.KeyScope, riG, rk *age.X25519Identity) {
	t.Helper()
	var err error
	if riG, err = age.GenerateX25519Identity(); err != nil {
		t.Fatal(err)
	}
	if rk, err = age.GenerateX25519Identity(); err != nil {
		t.Fatal(err)
	}
	if scope, err = crypto.ParseKeyScope(riG.Recipient().String(), rk.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	return scope, riG, rk
}

func openCatalog(t *testing.T) *catalog.DB {
	t.Helper()
	db, err := catalog.Open(t.Context(), filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testObserver records what a recorder reports.
type testObserver struct {
	mu        sync.Mutex
	restarts  []time.Time
	logs      []string
	firsts    []string // "<cam>/<profile>"
	firstAt   []time.Time
	verified  []ReconcileReport
	reports   []ReconcileReport // Reconciled, after a rewrite
	gaps      []catalog.Gap
	passedSeg []string
}

func (o *testObserver) ChildRestarted(at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.restarts = append(o.restarts, at)
}

func (o *testObserver) ChildLog(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logs = append(o.logs, line)
}

func (o *testObserver) FirstPart(cam CameraID, p Profile, at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.firsts = append(o.firsts, string(cam)+"/"+string(p))
	o.firstAt = append(o.firstAt, at)
}

func (o *testObserver) Verified(r ReconcileReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.verified = append(o.verified, r)
}

func (o *testObserver) Reconciled(r ReconcileReport) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reports = append(o.reports, r)
}

func (o *testObserver) Gap(g catalog.Gap) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gaps = append(o.gaps, g)
}

func (o *testObserver) SegmentVerified(path string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.passedSeg = append(o.passedSeg, path)
}

func (o *testObserver) snapshot() testObserver {
	o.mu.Lock()
	defer o.mu.Unlock()
	return testObserver{
		restarts: append([]time.Time(nil), o.restarts...), logs: append([]string(nil), o.logs...),
		firsts: append([]string(nil), o.firsts...), firstAt: append([]time.Time(nil), o.firstAt...),
		verified: append([]ReconcileReport(nil), o.verified...), reports: append([]ReconcileReport(nil), o.reports...),
		gaps: append([]catalog.Gap(nil), o.gaps...), passedSeg: append([]string(nil), o.passedSeg...),
	}
}

// startFake runs New with this test binary as a fake MediaMTX in mode, in a
// fresh data layout, and returns the recorder and its directories. The
// recorder stops when the test ends.
func startFake(t *testing.T, mode string, cfg Config) (*rec, Config) {
	t.Helper()
	t.Setenv(fakeChildEnv, mode)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg.BinaryPath = self
	if cfg.RunDir == "" {
		cfg.RunDir = filepath.Join(dir, "run")
	}
	if cfg.RecDir == "" {
		cfg.RecDir = filepath.Join(dir, "rec")
	}
	if cfg.Catalog == nil {
		cfg.Catalog = openCatalog(t)
	}
	if cfg.HookCommand == nil {
		cfg.HookCommand = []string{self, "hook", "segment-complete"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r, report, err := New(ctx, cfg)
	if err != nil {
		cancel()
		t.Fatalf("New: %v (report %+v)", err, report)
	}
	t.Cleanup(func() {
		cancel()
		for range r.Closed(context.Background()) {
		}
	})
	return r.(*rec), cfg
}

// The fMP4 recordings of the tests are written with mediacommon, as MediaMTX
// writes them (internal/recorder/format_fmp4_*.go): an init (ftyp, moov),
// then a moof and an mdat per part, here one H.264 track at 90 kHz with
// samples of 50 ms.
const (
	testTimeScale      = 90000
	testSampleDuration = 4500
)

var (
	testSPS = []byte{
		0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02,
		0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04,
		0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20,
	}
	testPPS = []byte{0x08, 0x06, 0x07, 0x08}
)

// fmp4File is a test recording and where its pieces lie.
type fmp4File struct {
	data      []byte
	initEnd   int64
	partStart []int64   // the offset of each part's moof
	partEnd   []int64   // the end of each part's mdat (and its free box, if any)
	sampleEnd [][]int64 // the end of each sample's data, per part
	payloads  [][][]byte
}

// fmp4Spec describes a test recording: parts of samples samples each, whose
// samples are sizes[i] bytes long in part i (each part is 1 s long).
type fmp4Spec struct {
	samples int
	sizes   []int
	// align pads each part with a free box to the next 64 KiB chunk
	// boundary, as MTX-2 pads a small encrypted part.
	align bool
}

// makeFMP4 writes a recording as MediaMTX does.
func makeFMP4(t *testing.T, spec fmp4Spec) fmp4File {
	t.Helper()
	var buf seekablebuffer.Buffer
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: testTimeScale, Codec: &codecs.H264{SPS: testSPS, PPS: testPPS}}}}
	if err := init.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	f := fmp4File{initEnd: int64(buf.Len())}
	for p, size := range spec.sizes {
		var part seekablebuffer.Buffer
		track := &fmp4.PartTrack{ID: 1, BaseTime: uint64(p * spec.samples * testSampleDuration)} //nolint:gosec // small test values
		var payloads [][]byte
		for s := range spec.samples {
			payload := bytes.Repeat([]byte{byte(p), byte(s)}, size/2)
			payloads = append(payloads, payload)
			track.Samples = append(track.Samples, &fmp4.Sample{Duration: testSampleDuration, IsNonSyncSample: s != 0, Payload: payload})
		}
		if err := (&fmp4.Part{SequenceNumber: uint32(p), Tracks: []*fmp4.PartTrack{track}}).Marshal(&part); err != nil { //nolint:gosec // small test values
			t.Fatal(err)
		}
		start := int64(buf.Len())
		b := part.Bytes()
		dataStart := start + int64(len(b)) - int64(spec.samples*len(payloads[0]))
		var ends []int64
		for s := range spec.samples {
			ends = append(ends, dataStart+int64((s+1)*len(payloads[0])))
		}
		_, _ = buf.Write(b) // a bytes.Buffer's Write never fails
		if spec.align {
			pad := (crypto.ChunkSize - buf.Len()%crypto.ChunkSize) % crypto.ChunkSize
			if pad > 0 && pad < 8 {
				pad += crypto.ChunkSize
			}
			if pad > 0 {
				_, _ = buf.Write(freeBox(pad))
			}
		}
		f.partStart = append(f.partStart, start)
		f.partEnd = append(f.partEnd, int64(buf.Len()))
		f.sampleEnd = append(f.sampleEnd, ends)
		f.payloads = append(f.payloads, payloads)
	}
	f.data = buf.Bytes()
	return f
}

// cutAge encrypts data whole and keeps the header, the payload nonce, the
// first chunks chunks and extra bytes of the next one: a cut that tore the
// chunk being written.
func cutAge(t *testing.T, data []byte, scope crypto.KeyScope, chunks, extra int) []byte {
	t.Helper()
	file := encryptFMP4(t, data, scope, nil, false)
	hdr, err := age.ExtractHeader(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	n := len(hdr) + 16 + chunks*(crypto.ChunkSize+16) + extra
	if n >= len(file) {
		t.Fatalf("a cut at %d bytes of a %d-byte file", n, len(file))
	}
	return file[:n]
}

func freeBox(size int) []byte {
	b := make([]byte, size)
	binary.BigEndian.PutUint32(b, uint32(size)) //nolint:gosec // small test values
	copy(b[4:], "free")
	return b
}

// encryptFMP4 writes data through crypto's age writer as MTX-2 does, with a
// Flush at each offset of flushAt (the part ends), and returns the bytes that
// reached the file: when torn, the writer is never closed, so only the
// chunks sealed so far are there.
func encryptFMP4(t *testing.T, data []byte, scope crypto.KeyScope, flushAt []int64, torn bool) []byte {
	t.Helper()
	var dst bytes.Buffer
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatal(err)
	}
	written := int64(0)
	for _, off := range flushAt {
		if off <= written || off > int64(len(data)) {
			continue
		}
		if _, err := w.Write(data[written:off]); err != nil {
			t.Fatal(err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		written = off
	}
	if _, err := w.Write(data[written:]); err != nil {
		t.Fatal(err)
	}
	if !torn {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return dst.Bytes()
}

// recordingPart is a part of a recording as mediacommon reads it.
type recordingPart struct {
	samples [][]byte
}

// parseRecording reads plaintext with mediacommon, independently of the code
// under test: the init, then each moof and mdat as one part, skipping free
// boxes. It fails the test unless every box is complete, so that a torn part
// cannot pass.
func parseRecording(t *testing.T, plaintext []byte) []recordingPart {
	t.Helper()
	var init fmp4.Init
	pos := int64(0)
	var parts []recordingPart
	moof := int64(-1)
	for pos < int64(len(plaintext)) {
		if int64(len(plaintext))-pos < 8 {
			t.Fatalf("%d bytes after the last box", int64(len(plaintext))-pos)
		}
		size := int64(binary.BigEndian.Uint32(plaintext[pos:]))
		typ := string(plaintext[pos+4 : pos+8])
		if size < 8 || pos+size > int64(len(plaintext)) {
			t.Fatalf("box %q at %d of size %d is torn (the file has %d bytes)", typ, pos, size, len(plaintext))
		}
		switch typ {
		case "moov":
			if err := init.Unmarshal(bytes.NewReader(plaintext[:pos+size])); err != nil {
				t.Fatalf("init: %v", err)
			}
		case "moof":
			moof = pos
		case "mdat":
			var ps fmp4.Parts
			if err := ps.Unmarshal(plaintext[moof : pos+size]); err != nil {
				t.Fatalf("part at %d: %v", moof, err)
			}
			for _, p := range ps {
				var rp recordingPart
				for _, s := range p.Tracks[0].Samples {
					rp.samples = append(rp.samples, s.Payload)
				}
				parts = append(parts, rp)
			}
			moof = -1
		}
		pos += size
	}
	if moof >= 0 {
		t.Fatalf("a moof at %d has no mdat", moof)
	}
	if len(init.Tracks) != 1 {
		t.Fatalf("the init has %d tracks", len(init.Tracks))
	}
	return parts
}

// requireParts checks that parts holds the first counts[i] samples of each
// part i of f.
func requireParts(t *testing.T, parts []recordingPart, f fmp4File, counts []int) {
	t.Helper()
	if len(parts) != len(counts) {
		t.Fatalf("%d parts, want %d", len(parts), len(counts))
	}
	for i, n := range counts {
		if len(parts[i].samples) != n {
			t.Fatalf("part %d has %d samples, want %d", i, len(parts[i].samples), n)
		}
		for s := range n {
			if !bytes.Equal(parts[i].samples[s], f.payloads[i][s]) {
				t.Fatalf("sample %d of part %d differs", s, i)
			}
		}
	}
}

// mvhdDuration returns the duration in the mvhd of a clear recording.
func mvhdDuration(t *testing.T, data []byte) uint64 {
	t.Helper()
	i := bytes.Index(data, []byte("mvhd"))
	if i < 0 {
		t.Fatal("no mvhd")
	}
	if data[i+4] == 1 {
		return binary.BigEndian.Uint64(data[i+4+4+8+8+4:])
	}
	return uint64(binary.BigEndian.Uint32(data[i+4+4+4+4+4:]))
}

// decryptAll decrypts a whole age file with id.
func decryptAll(t *testing.T, file []byte, id age.Identity) []byte {
	t.Helper()
	r, err := age.Decrypt(bytes.NewReader(file), id)
	if err != nil {
		t.Fatalf("age.Decrypt: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("age.Decrypt: read: %v", err)
	}
	return out
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// segmentName is the name MediaMTX gives a segment that starts at start, in
// the host's local time (recordPath …/%Y-%m-%d/%H%M%S_%f).
func segmentName(start time.Time) string {
	l := start.Local()
	return l.Format("2006-01-02") + "/" + l.Format("150405") + fmt.Sprintf("_%06d", l.Nanosecond()/1000) + ".mp4"
}

// The age CLIs of ENC-06, as crypto's tests read them from the environment.
const (
	envAgeBins         = "RECSPIKE_AGE_BINS"
	envRageBin         = "RECSPIKE_RAGE_BIN"
	envRequireAgeTools = "RECSPIKE_REQUIRE_AGE_TOOLS"
)

// decryptWithAgeTools decrypts file with each age CLI and rage, using an
// identity file that holds only id, and checks that each gives want. Without
// the tools it does nothing, unless they are required.
func decryptWithAgeTools(t *testing.T, file string, id *age.X25519Identity, want []byte) {
	t.Helper()
	bins := filepath.SplitList(os.Getenv(envAgeBins))
	rage := os.Getenv(envRageBin)
	if len(bins) == 0 || rage == "" {
		if os.Getenv(envRequireAgeTools) == "1" {
			t.Fatalf("%s=1, but %s or %s is empty", envRequireAgeTools, envAgeBins, envRageBin)
		}
		return
	}
	idFile := filepath.Join(t.TempDir(), "rk.key")
	writeFile(t, idFile, []byte(id.String()+"\n"))
	for _, bin := range append(bins, rage) {
		cmd := exec.CommandContext(t.Context(), bin, "--decrypt", "-i", idFile, file) //nolint:gosec // the tool comes from the job's environment
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		got, err := cmd.Output()
		if err != nil {
			t.Errorf("%s: %v: %s", bin, err, stderr.String())
		} else if !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes that differ from the %d expected", bin, len(got), len(want))
		}
	}
}

// setSyncDir replaces the directory sync until restore is called.
func setSyncDir(f func(dir string) error) (restore func()) {
	previous := syncDir
	syncDir = f
	return func() { syncDir = previous }
}

// setVerifyOpenHook makes each verification call f with the path of each
// segment it has opened, until restore is called.
func setVerifyOpenHook(f func(path string)) (restore func()) {
	previous := verifyOpenHook
	verifyOpenHook = f
	return func() { verifyOpenHook = previous }
}

// setRewriteHook makes each background rewrite call f as it starts, until
// restore is called.
func setRewriteHook(f func()) (restore func()) {
	previous := rewriteHook
	rewriteHook = f
	return func() { rewriteHook = previous }
}
