// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
)

// mi3Step is one step of MI3 that a host runs against this controller
// (G20, R07): its host's connection, its pacing's start, its live peers
// and their tiles' latencies.
type mi3Step struct {
	conn      *link.Conn
	start     controller.PacingMark
	peers     []*controller.LivePeer
	latencies *mi3.Latencies
}

// close closes the step's live peers.
func (st *mi3Step) close() {
	for _, p := range st.peers {
		p.Close()
	}
}

// maxLiveSessions bounds a step's live sessions: §4.8's most, H2's 4.
const maxLiveSessions = 4

// liveAcceptWait bounds the binds of every live tile of a step.
const liveAcceptWait = 90 * time.Second

// noLocalMI3 is why a -local controller refuses MI3: its cameras would load
// the host that MI3 measures, and its live peers would listen beyond
// 127.0.0.1.
const noLocalMI3 = "MI3 are nevoie de reccontroller pornit fără -local, pe celălalt dispozitiv"

// mi3Start starts a step of MI3: it marks the pacing (R07), makes the
// step's receive-only live peers, each a grid of the tiles, whose access
// units are timed against the send log (latency.go), and sends their
// offers; a step that it cannot serve is refused, in Romanian.
func (c *ctl) mi3Start(conn *link.Conn, m link.Mi3Start) {
	refuse := func(reason string) {
		c.say("Pasul MI3 %s nu se măsoară: %s.", m.RunID, reason)
		c.reply(conn, link.Refused{RunID: m.RunID, Reason: reason})
	}
	switch {
	case c.liveAPI == nil:
		refuse(noLocalMI3)
		return
	case m.Sessions < 1 || m.Sessions > maxLiveSessions:
		refuse(fmt.Sprintf("%d sesiuni live, nu între 1 și %d", m.Sessions, maxLiveSessions))
		return
	case len(m.Tiles) == 0:
		refuse("nicio placă live")
		return
	}
	for _, tile := range m.Tiles {
		if !slices.ContainsFunc(c.streams, func(s controller.Stream) bool { return s.Path == tile }) {
			refuse(fmt.Sprintf("controlorul nu servește %s", tile))
			return
		}
	}
	st := &mi3Step{conn: conn, latencies: mi3.NewLatencies(c.sendLog)}
	offers := make([]string, 0, m.Sessions)
	for range m.Sessions {
		p, err := controller.NewLivePeer(c.liveAPI, m.Tiles, c.clock, st.latencies.Arrived)
		if err != nil {
			st.close()
			refuse(fmt.Sprintf("nu pot pregăti sesiunile live: %v", err))
			return
		}
		st.peers = append(st.peers, p)
		offers = append(offers, p.Offer())
	}
	st.start = c.pacer.MarkPacing()
	c.mu.Lock()
	old := c.steps[m.RunID]
	c.steps[m.RunID] = st
	c.mu.Unlock()
	if old != nil {
		old.close() // started again: the latest start counts
	}
	c.say("Pasul MI3 %s a început: %d sesiuni live de câte %d plăci.", m.RunID, m.Sessions, len(m.Tiles))
	c.reply(conn, link.Mi3Offers{RunID: m.RunID, Offers: offers})
}

// mi3Answers applies the live load's answers and says so once every tile
// of every peer is bound; it runs on its own goroutine, since the binds
// take a moment.
func (c *ctl) mi3Answers(conn *link.Conn, m link.Mi3Answers) {
	refuse := func(reason string) {
		c.say("Pasul MI3 %s: %s.", m.RunID, reason)
		c.reply(conn, link.Refused{RunID: m.RunID, Reason: reason})
	}
	c.mu.Lock()
	st := c.steps[m.RunID]
	c.mu.Unlock()
	switch {
	case st == nil:
		refuse("pasul nu a fost anunțat controlorului")
		return
	case len(m.Answers) != len(st.peers):
		refuse(fmt.Sprintf("%d răspunsuri la %d sesiuni live", len(m.Answers), len(st.peers)))
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, liveAcceptWait)
	defer cancel()
	for i, p := range st.peers {
		if err := p.Accept(ctx, m.Answers[i]); err != nil {
			refuse(fmt.Sprintf("sesiunea live %d nu a pornit: %v", i+1, err))
			return
		}
	}
	c.say("Pasul MI3 %s: toate plăcile live primesc.", m.RunID)
	c.reply(conn, link.Mi3Live{RunID: m.RunID})
}

// mi3End ends a step: its pacing from its start to now (R07), its live
// tiles' added latency (§4.8), and its live peers closed.
func (c *ctl) mi3End(conn *link.Conn, m link.Mi3End) {
	c.mu.Lock()
	st := c.steps[m.RunID]
	delete(c.steps, m.RunID)
	c.mu.Unlock()
	if st == nil {
		c.reply(conn, link.Refused{RunID: m.RunID, Reason: "pasul nu a fost anunțat controlorului"})
		return
	}
	pacing := c.pacer.Pacing(st.start, c.pacer.MarkPacing(), c.clock.Now())
	st.close()
	p95, n := st.latencies.P95()
	res := link.Mi3Result{RunID: m.RunID, PacingP99Ms: pacing.P99.Milliseconds(), SkippedAUs: pacing.SkippedAUs,
		LatencyP95Ms: float64(p95.Milliseconds()), LatencySamples: n}
	c.say("Pasul MI3 %s s-a încheiat: ritm p99 %d ms, cadre sărite %d; latența plăcilor live p95 %d ms (%d cadre).",
		m.RunID, res.PacingP99Ms, res.SkippedAUs, p95.Milliseconds(), n)
	c.reply(conn, res)
}

// dropSteps closes the steps of a connection that ended: their host is
// gone.
func (c *ctl) dropSteps(conn *link.Conn) {
	c.mu.Lock()
	var gone []*mi3Step
	for id, st := range c.steps {
		if st.conn == conn {
			gone = append(gone, st)
			delete(c.steps, id)
		}
	}
	c.mu.Unlock()
	for _, st := range gone {
		st.close()
	}
}
