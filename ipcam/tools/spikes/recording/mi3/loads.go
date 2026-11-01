// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/session"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// The loads of MI3 (G20), each a child process `recspike mi3-load <kind>`,
// so that its CPU is its own.
const (
	LoadLive     = "live"
	LoadPlayback = "playback"
	LoadExport   = "export"
	LoadUpload   = "upload"
)

// Loads are the kinds of the loads, in the order they start.
var Loads = []string{LoadLive, LoadPlayback, LoadExport, LoadUpload}

// The rates of the loads: a camera's main stream is 4 Mbps, which the
// playback reads at 1×; the upload goes at 10 Mbps on H3 and H1.
const (
	cameraBps = 4_000_000
	uploadBps = 10_000_000
)

// UploadBps is the upload's rate on tier with cameras (G20, HUB-32): 10
// Mbps on H3 and H1; on H1b, H1c and an unknown model, the cameras' total
// throughput, 4 Mbps each.
func UploadBps(tier string, cameras int) int64 {
	switch tier {
	case TierH3, TierH1:
		return uploadBps
	}
	return int64(cameras) * cameraBps
}

// Proxies say what each load stands for, in Romanian, for the report (G20).
var Proxies = []string{
	"Încărcarea live (proxy): sesiunile nivelului, fiecare o grilă cu substream-ul fiecărei camere și un main, servite de pachetul session al " +
		"spike-ului video din recspike, din RTSP-ul controlorului, către câte un peer Pion doar de recepție în reccontroller; main se preia de două ori, " +
		"deci CPU-ul e o estimare acoperitoare. CPU-ul procesului live se împarte pe plăci după debitul lor (sub 512 kbps, main 4 Mbps).",
	"Redarea (proxy): citirea cu decriptare, la 1× (4 Mbps), a unui segment criptat de 5 min, în buclă. Segmentul este cel sintetic de mai jos, " +
		"nu un segment înregistrat și închis, și rămâne în cache-ul de pagini al sistemului: decriptarea costă la fel, dar citirea de pe disc nu se măsoară.",
	"Exportul (proxy): decriptarea și recriptarea, la viteză maximă, a unui segment de 5 min, repetată fără pauză tot timpul probei; ieșirea se aruncă.",
	"Upload-ul (proxy): criptare age și HTTPS PUT, cu ritm fix (10 Mbps; pe H1b și H1c, debitul camerelor), spre un receptor din reccontroller " +
		"care aruncă datele (fără cost, fără cont); CPU-ul se raportează la debitul atins.",
	"Segmentul de 5 min al redării, exportului și upload-ului are date aleatoare de mărimea unui main de 4 Mbps: costul criptării nu depinde de conținut.",
	"Procentele de CPU sunt din întreaga mașină (toate nucleele logice), ca CPU-ul total; ingest-ul per cameră (H1) este dintr-un nucleu, " +
		"din CPU-ul ipcam-hub și al MediaMTX.",
	"RAM (proxy): vârful sumei memoriei rezidente a ipcam-hub, MediaMTX și celor patru încărcări (pe Windows, setul de lucru): fiecare " +
		"încărcare are propriul runtime Go, deci cifra e acoperitoare.",
	"Părțile închise pe secundă per fișier (proxy): rândul de fsync pe secundă din §4.8 se măsoară ca (părți − 1) ÷ durata lor, din înregistrări; " +
		"MTX-1 face un fsync la fiecare parte de 1 s, deci cifra stă aproape de 1 prin construcție și nu vede un fsync în plus. Fsync-ul de la " +
		"închiderea segmentului, unul la 5 min, nu intră în ritm.",
}

// pacingClock is the clock of a paced reader.
type pacingClock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// systemTime is the system's clock.
type systemTime struct{}

func (systemTime) Now() time.Time        { return time.Now() }
func (systemTime) Sleep(d time.Duration) { time.Sleep(d) }

// pacedChunk bounds one read of a paced reader: 52 ms at 10 Mbps.
const pacedChunk = 64 << 10

// paced reads its reader no faster than bps bits per second, from its
// first read: after each read, it sleeps until the bytes read so far are
// due. sent is atomic: http's transport may still read a request's body
// after Do returns (net/http.RoundTripper), and the PUT reads its count
// then.
type paced struct {
	r     io.Reader
	bps   int64
	clock pacingClock
	start time.Time
	sent  atomic.Int64
}

func newPaced(r io.Reader, bps int64, clock pacingClock) *paced {
	return &paced{r: r, bps: bps, clock: clock}
}

func (p *paced) Read(b []byte) (int, error) {
	if p.start.IsZero() {
		p.start = p.clock.Now()
	}
	n, err := p.r.Read(b[:min(len(b), pacedChunk)])
	bits := p.sent.Add(int64(n)) * 8
	due := p.start.Add(time.Duration(bits/p.bps)*time.Second + time.Duration(bits%p.bps*int64(time.Second)/p.bps))
	if wait := due.Sub(p.clock.Now()); wait > 0 {
		p.clock.Sleep(wait)
	}
	return n, err
}

// LoadSpec is what a load child reads from its stdin, one JSON line.
// Sources map "<cam>/<profile>" to the controller's RTSP address of the
// stream and, with Offers, the live peers' offers, make the live load;
// Segment, an encrypted 5-min segment, and Identity, its key file, make the
// playback and the export; Plain, the same segment in clear, URL, the sink
// (https://<controller>:<port>/sink/), SinkCert, its certificate, and Bps
// make the upload. The addresses hold the RTSP credentials, so they travel
// on stdin, never on a command line.
type LoadSpec struct {
	Sources  map[string]string `json:"sources,omitempty"`
	Offers   []string          `json:"offers,omitempty"`
	Segment  string            `json:"segment,omitempty"`
	Identity string            `json:"identity,omitempty"`
	Plain    string            `json:"plain,omitempty"`
	URL      string            `json:"url,omitempty"`
	SinkCert []byte            `json:"sinkCert,omitempty"`
	Bps      int64             `json:"bps,omitempty"`
}

// LoadReport is what a load child prints on its stdout: once it runs, the
// live load's Answers to the offers (an empty report for the others); at its
// end, the bytes it moved in Seconds.
type LoadReport struct {
	Answers []string `json:"answers,omitempty"`
	Bytes   int64    `json:"bytes"`
	Seconds float64  `json:"seconds"`
}

// RunLoad runs the load kind of spec until ctx is done: the child's side of
// `recspike mi3-load`. It calls started with the first report once the load
// runs, and returns the last.
func RunLoad(ctx context.Context, kind string, spec LoadSpec, started func(LoadReport) error) (LoadReport, error) {
	begin := time.Now()
	var report LoadReport
	var err error
	switch kind {
	case LoadLive:
		err = runLive(ctx, spec, started)
	case LoadPlayback:
		report.Bytes, err = runPlayback(ctx, spec, started)
	case LoadExport:
		report.Bytes, err = runExport(ctx, spec, started)
	case LoadUpload:
		report.Bytes, err = runUpload(ctx, spec, started)
	default:
		return report, fmt.Errorf("mi3: unknown load %q", kind)
	}
	report.Seconds = time.Since(begin).Seconds()
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		err = nil // the parent ended the load
	}
	return report, err
}

// runLive answers each offer with a session of the video spike over the
// sources, read from the controller over RTSP once each, whatever the number
// of sessions, and serves them until ctx is done.
func runLive(ctx context.Context, spec LoadSpec, started func(LoadReport) error) error {
	sources := map[string]source.Source{}
	for key, url := range spec.Sources {
		src, err := source.NewRTSP(ctx, key, url)
		if err != nil {
			return err
		}
		sources[key] = src
	}
	api, _, err := session.NewAPI(false)
	if err != nil {
		return err
	}
	var answers []string
	for _, offer := range spec.Offers {
		s, answer, err := session.Answer(api, offer, sources)
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		answers = append(answers, answer)
	}
	if err := started(LoadReport{Answers: answers}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

// readIdentity reads the age identity in the file at path.
func readIdentity(path string) (age.Identity, error) {
	f, err := os.Open(path) //nolint:gosec // the run's own key file
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	ids, err := age.ParseIdentities(f)
	if err != nil {
		return nil, err
	}
	return ids[0], nil
}

// ctxReader reads r until ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(b)
}

// loop reads the segment in spec, decrypted, through each in turn, from its
// start again at each end, until ctx is done, and returns the plaintext
// bytes read.
func loop(ctx context.Context, spec LoadSpec, each func(plain io.Reader) (int64, error)) (int64, error) {
	id, err := readIdentity(spec.Identity)
	if err != nil {
		return 0, err
	}
	var total int64
	for {
		f, err := os.Open(spec.Segment)
		if err != nil {
			return total, err
		}
		plain, err := age.Decrypt(f, id)
		if err == nil {
			var n int64
			n, err = each(ctxReader{ctx: ctx, r: plain})
			total += n
		}
		_ = f.Close()
		if err != nil {
			return total, err
		}
	}
}

// runPlayback decrypts and reads the segment at 1×, 4 Mbps, in a loop: a
// viewer playing a closed recording.
func runPlayback(ctx context.Context, spec LoadSpec, started func(LoadReport) error) (int64, error) {
	if err := started(LoadReport{}); err != nil {
		return 0, err
	}
	return loop(ctx, spec, func(plain io.Reader) (int64, error) {
		return io.Copy(io.Discard, newPaced(plain, cameraBps, systemTime{}))
	})
}

// runExport decrypts the segment and encrypts it again to a fresh recipient
// at full speed, without pause, discarding the output: an export that never
// ends.
func runExport(ctx context.Context, spec LoadSpec, started func(LoadReport) error) (int64, error) {
	to, err := age.GenerateX25519Identity()
	if err != nil {
		return 0, err
	}
	if err := started(LoadReport{}); err != nil {
		return 0, err
	}
	return loop(ctx, spec, func(plain io.Reader) (int64, error) {
		w, err := age.Encrypt(io.Discard, to.Recipient())
		if err != nil {
			return 0, err
		}
		n, err := io.Copy(w, plain)
		if err != nil {
			return n, err
		}
		return n, w.Close()
	})
}

// runUpload encrypts the clear segment with age for a fresh recipient and
// PUTs it to the sink at spec.Bps, over and over, until ctx is done; it
// returns the bytes that went out. Once ctx is done, the PUT that its end
// cut short ends the load whatever comes back, the sink's 400 for a body
// that did not arrive or the connection's error: that is the load's end,
// not a failure.
func runUpload(ctx context.Context, spec LoadSpec, started func(LoadReport) error) (int64, error) {
	tlsConfig, err := controller.SinkClientTLS(spec.SinkCert)
	if err != nil {
		return 0, err
	}
	to, err := age.GenerateX25519Identity()
	if err != nil {
		return 0, err
	}
	client := &http.Client{Transport: uploadTransport(tlsConfig)}
	defer client.CloseIdleConnections()
	if err := started(LoadReport{}); err != nil {
		return 0, err
	}
	var total int64
	for n := 1; ; n++ {
		sent, err := putSegment(ctx, client, spec, to.Recipient(), fmt.Sprintf("%supload-%d", spec.URL, n))
		total += sent
		if err != nil && ctx.Err() != nil {
			return total, ctx.Err()
		}
		if err != nil {
			return total, err
		}
	}
}

// uploadTransport is the upload's HTTP transport, with tlsConfig; tests
// replace it.
var uploadTransport = func(tlsConfig *tls.Config) http.RoundTripper {
	return &http.Transport{TLSClientConfig: tlsConfig}
}

// putSegment PUTs the clear segment, age-encrypted as it goes, at the
// paced rate, and checks that the sink counted every byte.
func putSegment(ctx context.Context, client *http.Client, spec LoadSpec, to age.Recipient, url string) (int64, error) {
	f, err := os.Open(spec.Plain)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	pr, pw := io.Pipe()
	go func() {
		w, err := age.Encrypt(pw, to)
		if err == nil {
			_, err = io.Copy(w, ctxReader{ctx: ctx, r: f})
		}
		if err == nil {
			err = w.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	body := newPaced(pr, spec.Bps, systemTime{})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		_ = pr.Close()
		return 0, err
	}
	res, err := client.Do(req)
	_ = pr.Close() // ends the encryption, if the PUT ended first
	if err != nil {
		return body.sent.Load(), err
	}
	defer func() { _ = res.Body.Close() }()
	var got struct {
		Bytes int64 `json:"bytes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil || res.StatusCode != http.StatusOK {
		return body.sent.Load(), fmt.Errorf("the sink answered %s (%v)", res.Status, err)
	}
	sent := body.sent.Load()
	if got.Bytes != sent {
		return sent, fmt.Errorf("the sink counted %d bytes of %d", got.Bytes, sent)
	}
	return sent, nil
}

// splitTile is a tile's stream, "cam01_sub", as the live load's source
// key, "cam01/sub".
func splitTile(stream string) string { return strings.Replace(stream, "_", "/", 1) }

// load is a load child as a step runs it: `recspike mi3-load <kind>`,
// with its spec on stdin, its reports on stdout and its log on stderr.
type load struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	first   LoadReport
	last    chan LoadReport // the report at its end
	exited  chan struct{}   // closed once it has exited, waitErr set
	waitErr error

	stopOnce sync.Once
	report   LoadReport
	stopErr  error
}

// loadStopWait bounds a load's end after its stdin closed; then it is
// killed.
const loadStopWait = 30 * time.Second

// startLoad starts the load kind of spec with self, recspike, and waits for
// its first report.
func startLoad(ctx context.Context, self, kind string, spec LoadSpec, stderr io.Writer) (*load, error) {
	cmd := exec.Command(self, "mi3-load", kind) //nolint:gosec // recspike itself
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ld := &load{cmd: cmd, stdin: stdin, last: make(chan LoadReport, 1), exited: make(chan struct{})}
	first := make(chan LoadReport, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 4<<20) // the live load's answers
		// Only the lines that are reports count: a stray line, such as a
		// library's, is no first report.
		reports := 0
		for sc.Scan() {
			var r LoadReport
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue
			}
			if reports == 0 {
				first <- r
			} else {
				select {
				case ld.last <- r:
				default:
				}
			}
			reports++
		}
		ld.waitErr = cmd.Wait()
		close(ld.exited)
	}()
	data, err := json.Marshal(spec)
	if err == nil {
		_, err = stdin.Write(append(data, '\n'))
	}
	if err != nil {
		_, _ = ld.stop()
		return nil, err
	}
	timeout := time.NewTimer(loadStartWait)
	defer timeout.Stop()
	select {
	case ld.first = <-first:
		return ld, nil
	case <-ld.exited:
		_ = stdin.Close()
		return nil, fmt.Errorf("s-a oprit înainte să pornească (%v)", ld.waitErr)
	case <-timeout.C:
		_, _ = ld.stop()
		return nil, fmt.Errorf("nu a pornit în %v", loadStartWait)
	case <-ctx.Done():
		_, _ = ld.stop()
		return nil, ctx.Err()
	}
}

// ended reports whether the load has exited.
func (ld *load) ended() bool {
	select {
	case <-ld.exited:
		return true
	default:
		return false
	}
}

// stop ends the load, closing its stdin, which the child takes as its end,
// and returns its last report; a load that does not end within
// loadStopWait is killed. Only the first call stops it.
func (ld *load) stop() (LoadReport, error) {
	ld.stopOnce.Do(func() {
		_ = ld.stdin.Close()
		select {
		case <-ld.exited:
		case <-time.After(loadStopWait):
			_ = ld.cmd.Process.Kill()
			<-ld.exited
		}
		select {
		case ld.report = <-ld.last:
			ld.stopErr = ld.waitErr
		default:
			ld.stopErr = fmt.Errorf("nu a trimis raportul de final (%v)", ld.waitErr)
		}
	})
	return ld.report, ld.stopErr
}
