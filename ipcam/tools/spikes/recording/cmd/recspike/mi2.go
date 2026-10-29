// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi2"
)

// mi2Command is `recspike mi2 --controller 127.0.0.1 [--hours 72]`: MI2's
// 72 h run on this host (G11), with the cameras of a reccontroller -local on
// the same device (R07, G18, G19): a controller at another address is
// refused, before anything runs.
func mi2Command(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("mi2", flag.ContinueOnError)
	controller := flags.String("controller", "", "")
	hours := flags.Float64("hours", mi2.RunHours, "")
	if err := parseFlags(flags, args, out); err != nil {
		return err
	}
	switch {
	case *controller == "":
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește --controller: adresa reccontroller -local, 127.0.0.1")
	case isIPv6(*controller):
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("--controller %s este o adresă IPv6: reccontroller ascultă doar pe IPv4; folosiți 127.0.0.1", *controller)
	case !ipv4Loopback(*controller):
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("--controller %s nu este pe acest dispozitiv: proba de 72 h folosește reccontroller -local, pornit pe același dispozitiv, la 127.0.0.1", *controller)
	case *hours <= 0:
		_, _ = fmt.Fprint(out, usage)
		return errors.New("--hours este un număr de ore mai mare decât 0")
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	// The host's recommended camera count, its storage and, on an SD card,
	// the owner's confirmation (C24, G16).
	host, err := mi1.OwnerHost(ctx, f.hub, f.work, runtime.GOOS, in, out)
	if err != nil {
		return err
	}
	addr := controllerAddr(*controller)
	return mi2.Run(ctx, mi2.Options{Hours: *hours, Host: host, Hub: f.hub, Results: f.results, Work: f.work, Out: out,
		CameraURLs: func(ctx context.Context, streams []string) (map[string]string, error) {
			return mi1.CameraURLs(ctx, addr, host.Pseudonym, streams)
		}})
}

// controllerHost is the host of addr, an address with or without a port,
// and its IP when it is one.
func controllerHost(addr string) (string, net.IP) {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return host, net.ParseIP(host)
}

// isIPv6 reports whether addr's host is an IPv6 address: reccontroller
// listens on IPv4 only (R08).
func isIPv6(addr string) bool {
	_, ip := controllerHost(addr)
	return ip != nil && ip.To4() == nil
}

// ipv4Loopback reports whether addr, a host with or without a port, is this
// device's IPv4 loopback, where reccontroller -local listens (R08): an
// address in 127.0.0.0/8, or localhost.
func ipv4Loopback(addr string) bool {
	host, ip := controllerHost(addr)
	if ip != nil {
		return ip.To4() != nil && ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}
