// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command recspike is the recorder spike's program on the host under test
// (T43). It runs ipcam-hub next to it, connects out to reccontroller on the
// other device (R08), and runs MI1's sets: the automated stops inside the
// owner's window, the owner's real cuts, and the lab's shards, MI2's 72 h
// run against a reccontroller on the same device, and MI3's capacity run
// against the one on the other device, with its loads as recspike mi3-load
// children, MI4's age rig against the PC's, and MI9's LAN run, whose
// recording the other device's reccontroller plays; it installs the autostart
// that continues a set after each reboot (G14), and pushes
// the Pi's results to the PC at the end (G15). Its results folder,
// results/ next to it, is published; its private folder, work/, holds the
// runs' recordings and keys and the controller's address. Every line it
// prints is Romanian.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
)

// usage is the command's help, in Romanian.
const usage = `Folosire:
  recspike install
      instalează pornirea automată după fiecare repornire (o dată, ca administrator pe PC, cu sudo pe Pi).
  recspike uninstall
      șterge pornirea automată și scurtătura de pe desktop.
  recspike resume
      continuă proba după o repornire (îl pornește pornirea automată).
  recspike mi1 --controller ADRESA --window HH:MM-HH:MM --kind kill9|sysrq|bugcheck
      opririle automate, doar în fereastra dată (pe PC: kill9 sau bugcheck; pe Pi: kill9 sau sysrq).
  recspike mi1 --controller ADRESA --real [--resume]
      cele 5 tăieri reale de alimentare; --resume continuă după o repornire.
  recspike mi1 --lab --kind kill9|qemu-reset --shard 1|2 [--smoke] [--controller ADRESA]
      o jumătate de set în laborator (GitHub), pe 127.0.0.1 dacă nu se dă alt controlor; qemu-reset fără --controller
      pornește mașina virtuală care rulează shardul și o resetează la fiecare oprire.
  recspike mi2 --controller 127.0.0.1 [--hours 72]
      proba de 72 h (MI2): înregistrează numărul recomandat de camere al gazdei, de la reccontroller -local
      pornit pe același dispozitiv; lăsați dispozitivul pornit tot timpul.
  recspike mi3 --controller ADRESA
      proba de capacitate (MI3): numărul recomandat de camere al gazdei, de la reccontroller de pe celălalt
      dispozitiv, cu sesiunile live, redarea, exportul și upload-ul în paralel, 1 h (plus pașii, dacă pică).
  recspike mi3-load live|playback|export|upload
      intern: o încărcare a probei MI3, pornită de recspike mi3.
  recspike mi4 --controller ADRESA_PC
      proba MI4 (citiri, salturi și criptare age): înregistrează un segment criptat de 5 min de la reccontroller
      de pe PC, apoi măsoară. Pe Pi se pornește cu sudo (sudo ./recspike mi4 …), care golește memoria cache
      a discului înaintea fiecărui set de citiri și salturi.
  recspike mi9 --lan --controller ADRESA
      proba MI9 pe LAN (redare prin canalul de date): înregistrează un segment criptat de 5 min de la reccontroller
      de pe celălalt dispozitiv (pe Pi îl ia pe al probei MI4, dacă a rămas unul), apoi reccontroller îl redă și
      măsoară debitul și 20 de salturi. Porniți-o o dată pe fiecare dispozitiv: întâi pe Pi, apoi pe PC.
  recspike push --controller ADRESA_PC
      trimite rezultatele Pi-ului către reccontroller pull de pe PC.
  recspike annotate split PLIC.json… --first PARTEA --out DOSAR
      în laborator: câte o linie pe rulare validă a shardului, cu partea ei din set, de cel mult 3.500 de octeți.
  recspike annotate emit DOSAR --batch K
      tipărește liniile 10(K−1)+1 … 10K din DOSAR: cel mult 10 notificări GitHub pe pas.
  recspike annotate read DOSAR|FIȘIER
      citește notificările salvate cu gh api …/annotations și arată, pe set, câte părți au venit.
`

func main() {
	// A set, MI2's 72 h and MI3's hours outlive an ssh window easily
	// (report-1); every subcommand ignores the hangup alike.
	cli.IgnoreHangup()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := report(run(ctx, os.Args[1:], bufio.NewReader(os.Stdin), os.Stdout), os.Stderr)
	stop()
	os.Exit(code)
}

// report prints the last line of a run that ended with err on stderr, and
// returns recspike's exit code: 0 without an error, else 1. The owner's
// stop (Ctrl+C), whose error is context.Canceled, ends in a Romanian line
// of its own: the probe has already said what it kept. MI4's refusal
// without sudo is a whole line that says what to run, printed alone.
func report(err error, stderr io.Writer) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
	case errors.Is(err, context.Canceled):
		_, _ = fmt.Fprintln(stderr, "Proba a fost oprită de dvs.")
	case errors.Is(err, mi4.ErrNeedsSudo):
		_, _ = fmt.Fprintln(stderr, err)
	default:
		_, _ = fmt.Fprintln(stderr, "recspike: eroare:", err)
	}
	return 1
}

// run runs the command in args; in is the owner's keyboard.
func run(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește comanda")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(out, usage)
		return flag.ErrHelp
	case "mi1":
		return mi1Command(ctx, rest, in, out)
	case "mi2":
		return mi2Command(ctx, rest, in, out)
	case "mi3":
		return mi3Command(ctx, rest, in, out)
	case "mi3-load":
		return mi3LoadCommand(ctx, rest, in, out)
	case "mi4":
		return mi4Command(ctx, rest, in, out)
	case "mi9":
		return mi9Command(ctx, rest, in, out)
	case "resume":
		return resumeCommand(ctx, rest, out)
	case "push":
		return pushCommand(ctx, rest, out)
	case "annotate":
		return annotateCommand(rest, out)
	case "install", "uninstall":
		if err := parseFlags(flag.NewFlagSet(cmd, flag.ContinueOnError), rest, out); err != nil {
			return err
		}
		exe, err := executable()
		if err != nil {
			return err
		}
		a := systemAutostart(out)
		if cmd == "install" {
			return a.install(exe)
		}
		return a.uninstall()
	}
	_, _ = fmt.Fprint(out, usage)
	return fmt.Errorf("comandă necunoscută: %s", cmd)
}

// parseFlags parses a subcommand's flags with recspike's Romanian usage.
func parseFlags(flags *flag.FlagSet, args []string, out io.Writer) error {
	return cli.ParseFlags(flags, args, out, usage)
}

// folders are recspike's files: the binaries next to it, results/ and
// work/.
type folders struct {
	dir, hub, results, work string
}

// executable is recspike's own path; tests put it in a folder of theirs.
var executable = os.Executable

func ownFolders() (folders, error) {
	exe, err := executable()
	if err != nil {
		return folders{}, err
	}
	dir := filepath.Dir(exe)
	hub := filepath.Join(dir, "ipcam-hub")
	if runtime.GOOS == "windows" {
		hub += ".exe"
	}
	return folders{dir: dir, hub: hub, results: filepath.Join(dir, "results"), work: filepath.Join(dir, "work")}, nil
}

// controllerFile keeps the controller's address in the private folder,
// for resume, durably: results/ is published and names no address
// (PH0-22).
func (f folders) controllerFile() string { return filepath.Join(f.work, "controller") }

func (f folders) saveController(addr string) error {
	if err := os.MkdirAll(f.work, 0o700); err != nil {
		return err
	}
	return mi1.WriteDurably(f.controllerFile(), []byte(addr+"\n"), 0o600)
}

func (f folders) savedController() (string, error) {
	data, err := os.ReadFile(f.controllerFile())
	if err != nil {
		return "", errors.New("nu știu adresa controlorului: porniți întâi proba cu --controller")
	}
	return strings.TrimSpace(string(data)), nil
}

// controllerAddr is the controller's link address: the address given, with
// the link's port when it has none.
func controllerAddr(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, strconv.Itoa(link.Port))
}

// ownerHost is the owner's device pseudonym on this OS: the Windows PC or
// the Raspberry Pi.
func ownerHost() string {
	if runtime.GOOS == "windows" {
		return "pc"
	}
	return "pi"
}
