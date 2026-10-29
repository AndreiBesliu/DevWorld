// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// noLocalMI9 is why a -local controller refuses MI9: the run plays the
// recording on the other device, and its Pion sockets would listen beyond
// 127.0.0.1.
const noLocalMI9 = "MI9 are nevoie de reccontroller pornit fără -local, pe celălalt dispozitiv"

// The waits of an MI9 run: the rpc channel's opening after the host's
// answer, and the measurement, a 5-min segment's plaintext and 20 seeks.
const (
	mi9AcceptWait  = 90 * time.Second
	mi9MeasureWait = 15 * time.Minute
)

// mi9Run is one LAN run of MI9 that a host serves to this controller (G26
// leg 3): its host's connection and the playback peer.
type mi9Run struct {
	conn   *link.Conn
	client *mi9.Client
}

// mi9Start starts a LAN run of MI9: the controller makes its playback
// peer, which offers; a run that it cannot play is refused, in Romanian.
func (c *ctl) mi9Start(conn *link.Conn, m link.Mi9Start) {
	refuse := func(reason string) {
		c.say("Proba MI9 %s nu se măsoară: %s.", m.RunID, reason)
		c.reply(conn, link.Refused{RunID: m.RunID, Reason: reason})
	}
	if c.mi9API == nil {
		refuse(noLocalMI9)
		return
	}
	client, err := mi9.NewClient(c.mi9API, mi9.Configuration(mi9.LAN, nil))
	if err != nil {
		refuse(fmt.Sprintf("nu pot pregăti redarea: %v", err))
		return
	}
	c.mu.Lock()
	old := c.mi9Runs[m.RunID]
	c.mi9Runs[m.RunID] = &mi9Run{conn: conn, client: client}
	c.mu.Unlock()
	if old != nil {
		_ = old.client.Close() // started again: the latest start counts
	}
	c.say("Proba MI9 %s: gazda îmi dă înregistrarea; o redau prin canalul de date.", m.RunID)
	c.reply(conn, link.Mi9Offer{RunID: m.RunID, Offer: client.Offer()})
}

// mi9Answer applies the host's answer and measures the bulk channel: the
// whole segment, then the seeks. It runs on its own goroutine, under ctx,
// the connection's: once the host is gone, the measurement ends with it.
func (c *ctl) mi9Answer(ctx context.Context, conn *link.Conn, m link.Mi9Answer) {
	c.mu.Lock()
	run := c.mi9Runs[m.RunID]
	delete(c.mi9Runs, m.RunID)
	c.mu.Unlock()
	if run == nil {
		c.say("Proba MI9 %s: proba nu a fost anunțată controlorului.", m.RunID)
		c.reply(conn, link.Refused{RunID: m.RunID, Reason: "proba nu a fost anunțată controlorului"})
		return
	}
	defer func() { _ = run.client.Close() }()
	acceptCtx, cancel := context.WithTimeout(ctx, mi9AcceptWait)
	err := run.client.Accept(acceptCtx, m.Answer)
	cancel()
	var r mi9.BulkResult
	if err == nil {
		measureCtx, cancel := context.WithTimeout(ctx, mi9MeasureWait)
		r, err = run.client.Measure(measureCtx)
		cancel()
	}
	switch {
	case ctx.Err() != nil:
		c.say("Proba MI9 %s: gazda s-a deconectat înainte de final.", m.RunID)
		return
	case err != nil:
		reason := fmt.Sprintf("redarea nu a reușit: %v", err)
		c.say("Proba MI9 %s: %s.", m.RunID, reason)
		c.reply(conn, link.Refused{RunID: m.RunID, Reason: reason})
		return
	}
	c.say("Proba MI9 %s: debit %s Mbps; primul cadru întreg după salt: max %s ms (%d salturi); RTT %s ms.",
		m.RunID, results.Decimal(r.ThroughputMbps, 1), results.Decimal(slices.Max(r.SeekMs), -1), len(r.SeekMs), results.Decimal(r.RTTMs, -1))
	c.reply(conn, link.Mi9Result{RunID: m.RunID, ThroughputMbps: r.ThroughputMbps, SeekMs: r.SeekMs, RTTMs: r.RTTMs})
}

// dropMI9 closes the playback peers of a connection that ended before its
// answer.
func (c *ctl) dropMI9(conn *link.Conn) {
	c.mu.Lock()
	var gone []*mi9Run
	for id, run := range c.mi9Runs {
		if run.conn == conn {
			gone = append(gone, run)
			delete(c.mi9Runs, id)
		}
	}
	c.mu.Unlock()
	for _, run := range gone {
		_ = run.client.Close()
	}
}
