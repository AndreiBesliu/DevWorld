// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command recspike is the recorder spike's program on the host under test
// (T43). It runs ipcam-hub next to it, connects out to reccontroller on the
// other device (R08), and runs MI1's sets: the automated stops inside the
// owner's window, the owner's real cuts, and the lab's shards; it installs
// the autostart that continues a set after each reboot (G14), and pushes
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

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
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
      o jumătate de set în laborator (GitHub), pe 127.0.0.1 dacă nu se dă alt controlor.
  recspike push --controller ADRESA_PC
      trimite rezultatele Pi-ului către reccontroller pull de pe PC.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], bufio.NewReader(os.Stdin), os.Stdout); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "recspike: eroare:", err)
		}
		stop()
		os.Exit(1)
	}
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
	case "resume":
		return resumeCommand(ctx, rest, out)
	case "push":
		return pushCommand(ctx, rest, out)
	case "install", "uninstall":
		if err := parseFlags(flag.NewFlagSet(cmd, flag.ContinueOnError), rest, out); err != nil {
			return err
		}
		exe, err := os.Executable()
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

// parseFlags parses args and, on an error, prints the usage and returns the
// error in Romanian, never the flag package's English.
func parseFlags(flags *flag.FlagSet, args []string, out io.Writer) error {
	flags.SetOutput(io.Discard)
	err := flags.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		_, _ = fmt.Fprint(out, usage)
		return err
	case err != nil:
		_, _ = fmt.Fprint(out, usage)
		return errors.New(romanianFlagError(err))
	case flags.NArg() > 0:
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("argument necunoscut: %s", flags.Arg(0))
	}
	return nil
}

// romanianFlagError translates the flag package's errors.
func romanianFlagError(err error) string {
	msg := err.Error()
	for _, t := range []struct{ en, ro string }{
		{"flag provided but not defined: ", "opțiune necunoscută: "},
		{"flag needs an argument: ", "opțiunea cere o valoare: "},
		{"bad flag syntax: ", "opțiune scrisă greșit: "},
	} {
		if rest, ok := strings.CutPrefix(msg, t.en); ok {
			return t.ro + rest
		}
	}
	if strings.HasPrefix(msg, "invalid ") {
		if _, rest, ok := strings.Cut(msg, " for "); ok {
			flagName, _, _ := strings.Cut(strings.TrimPrefix(rest, "flag "), ":")
			return "valoare greșită pentru " + flagName
		}
	}
	return "opțiuni greșite: " + msg
}

// folders are recspike's files: the binaries next to it, results/ and
// work/.
type folders struct {
	dir, hub, results, work string
}

func ownFolders() (folders, error) {
	exe, err := os.Executable()
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
// for resume: results/ is published and names no address (PH0-22).
func (f folders) controllerFile() string { return filepath.Join(f.work, "controller") }

func (f folders) saveController(addr string) error {
	if err := os.MkdirAll(f.work, 0o700); err != nil {
		return err
	}
	return os.WriteFile(f.controllerFile(), []byte(addr+"\n"), 0o600)
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
