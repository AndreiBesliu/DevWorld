// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// The ports of reccontroller's listeners (R08), besides the link's and the
// beacon's, which package link names.
const (
	RTSPPort   = 8554
	AppPort    = 7080
	UploadPort = 7083
)

// ListenConfig says where reccontroller listens. Host "" is every IPv4
// interface; --local uses 127.0.0.1 (G19). A port of 0 picks a free one.
type ListenConfig struct {
	Host                                                string
	RTSPPort, LinkPort, BeaconPort, AppPort, UploadPort int
}

// DefaultListenConfig is reccontroller's configuration: the fixed ports on
// every IPv4 interface, or on 127.0.0.1 for a host-local controller.
func DefaultListenConfig(local bool) ListenConfig {
	cfg := ListenConfig{RTSPPort: RTSPPort, LinkPort: link.Port, BeaconPort: link.BeaconPort, AppPort: AppPort, UploadPort: UploadPort}
	if local {
		cfg.Host = "127.0.0.1"
	}
	return cfg
}

// Listeners are every inbound socket of the spike (R08): the host side only
// connects out.
type Listeners struct {
	RTSP, Link, App, Upload net.Listener
	Beacon                  net.PacketConn
}

// ListenAll opens every listener with tcp4 or udp4 only: a "tcp" listener on
// 0.0.0.0 would be a dual-stack IPv6 socket (R25, R38). On an error, the
// listeners opened so far are closed.
func ListenAll(ctx context.Context, cfg ListenConfig) (*Listeners, error) {
	host := cfg.Host
	if host == "" {
		host = "0.0.0.0"
	}
	addr := func(port int) string { return net.JoinHostPort(host, strconv.Itoa(port)) }
	var lc net.ListenConfig
	l := &Listeners{}
	var err error
	if l.RTSP, err = lc.Listen(ctx, "tcp4", addr(cfg.RTSPPort)); err != nil {
		return nil, errors.Join(err, l.Close())
	}
	if l.Link, err = lc.Listen(ctx, "tcp4", addr(cfg.LinkPort)); err != nil {
		return nil, errors.Join(err, l.Close())
	}
	if l.Beacon, err = lc.ListenPacket(ctx, "udp4", addr(cfg.BeaconPort)); err != nil {
		return nil, errors.Join(err, l.Close())
	}
	if l.App, err = lc.Listen(ctx, "tcp4", addr(cfg.AppPort)); err != nil {
		return nil, errors.Join(err, l.Close())
	}
	if l.Upload, err = lc.Listen(ctx, "tcp4", addr(cfg.UploadPort)); err != nil {
		return nil, errors.Join(err, l.Close())
	}
	return l, nil
}

// listenTCP4 opens one IPv4 TCP listener at addr.
func listenTCP4(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp4", addr)
}

// Close closes every listener that is open.
func (l *Listeners) Close() error {
	var errs []error
	for _, c := range []io.Closer{l.RTSP, l.Link, l.Beacon, l.App, l.Upload} {
		if c != nil {
			errs = append(errs, c.Close())
		}
	}
	return errors.Join(errs...)
}
