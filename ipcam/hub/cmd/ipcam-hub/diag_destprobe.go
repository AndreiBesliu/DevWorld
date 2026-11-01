package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// destProbeSchema names the output of diag dest-probe.
const destProbeSchema = "ipcam.diag.destprobe/1"

// destProbeReport is diag dest-probe's line.
type destProbeReport struct {
	Schema string       `json:"schema"`
	Dest   string       `json:"dest"`
	Kind   storage.Kind `json:"kind"`
	Probe  probeFields  `json:"probe"`
	// QuotaAfterDelete is the quota after --delete-segment's delete, read
	// every --poll for --for: free is what remains, inBin what the recycle
	// bin holds (P35).
	QuotaAfterDelete []quotaSample `json:"quotaAfterDelete"`
}

type probeFields struct {
	Writable  bool        `json:"writable"`
	RangeRead bool        `json:"rangeRead"`
	Deletable bool        `json:"deletable"`
	Quota     quotaFields `json:"quota"`
	Warnings  []string    `json:"warnings"`
}

type quotaFields struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
	InBin int64 `json:"inBin"`
}

// quotaSample is one quota read after the delete, tMs after it.
type quotaSample struct {
	TMs   int64 `json:"tMs"`
	Free  int64 `json:"free"`
	InBin int64 `json:"inBin"`
}

// diagDestProbeCommand is "ipcam-hub diag dest-probe --config <run.json>
// --dest <id> [--delete-segment] [--poll 30s] [--for 10m]" (P35): it runs the
// destination's Probe and prints one ipcam.diag.destprobe/1 line. With
// --delete-segment it then deletes the copy of the destination's oldest
// verified placement, marks the placement deleted, and reads the quota right
// away and every --poll until --for has passed. It opens the catalog that
// a run left, the Drive driver's folder store, and reads state/hub_id,
// creating neither (g2-11). It refuses while ipcam-hub holds the hub lock
// (C2-R11), since Drive's creates of both would pass 2 a second, and holds
// that lock itself while it runs. It exits 1 on a usage error, a refusal,
// or a failed probe or delete, whose line still goes out when the probe ran.
func diagDestProbeCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag dest-probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "the run configuration (run.json)")
	destID := flags.String("dest", "", "the destination's id")
	deleteSegment := flags.Bool("delete-segment", false, "delete the oldest verified copy, then read the quota")
	poll := flags.Duration("poll", 30*time.Second, "how often to read the quota after the delete")
	readFor := flags.Duration("for", 10*time.Minute, "how long to read the quota after the delete")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if *config == "" || *destID == "" || *poll <= 0 || *readFor < 0 || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	failed := func(err error) int {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag dest-probe:", err)
		return exitFatal
	}
	dataDir, uploads, hubID, err := uploadsOf(*config)
	if err != nil {
		return failed(err)
	}
	def, ok := uploads.destByID(*destID)
	if !ok {
		return failed(fmt.Errorf("no destination %q in the uploads section", *destID))
	}
	release, err := lockHub(filepath.Join(dataDir, "state"))
	if errors.Is(err, errHubRunning) {
		return failed(fmt.Errorf("%w: it refuses while ipcam-hub runs", err))
	}
	if err != nil {
		return failed(err)
	}
	defer release()
	ctx := context.Background()
	cat, err := openExistingCatalog(ctx, dataDir)
	if err != nil {
		return failed(err)
	}
	defer func() { _ = cat.Close() }()
	d, err := newDestination(def, hubID, cat, newHTTPClient(hubTransport()))
	if err != nil {
		return failed(err)
	}
	report := destProbeReport{Schema: destProbeSchema, Dest: def.ID, Kind: def.Kind, QuotaAfterDelete: []quotaSample{}}
	probe, err := d.Probe(ctx)
	report.Probe = probeFields{Writable: probe.Writable, RangeRead: probe.RangeRead, Deletable: probe.Deletable,
		Quota:    quotaFields{Total: probe.Quota.Total, Used: probe.Quota.Used, Free: probe.Quota.Free, InBin: probe.Quota.InBin},
		Warnings: append([]string{}, probe.Warnings...)}
	if err == nil && *deleteSegment {
		report.QuotaAfterDelete, err = deleteOldest(ctx, cat, d, def.ID, hubID, uploads.HubName, *poll, *readFor)
	}
	if code := printJSONLine(stdout, stderr, report); code != exitOK {
		return code
	}
	if err != nil {
		return failed(err)
	}
	return exitOK
}

// deleteOldest deletes the copy of destID's oldest verified placement, by its
// segment's start, marks the placement deleted, and returns the quota read
// right after and every poll until readFor has passed.
func deleteOldest(ctx context.Context, cat *catalog.DB, d storage.Destination, destID, hubID, hubName string, poll, readFor time.Duration) ([]quotaSample, error) {
	segs, err := cat.Segments(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := cat.Placements(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]catalog.Segment{}
	for _, s := range segs {
		byID[s.ID] = s
	}
	var oldest *catalog.Placement
	for i, p := range ps {
		if p.DestinationID != destID || p.State != catalog.PlacementVerified {
			continue
		}
		if oldest == nil || byID[p.ObjectID].StartMs < byID[oldest.ObjectID].StartMs {
			oldest = &ps[i]
		}
	}
	if oldest == nil {
		return nil, fmt.Errorf("no verified placement on %s to delete", destID)
	}
	seg := byID[oldest.ObjectID]
	ref := storage.ObjectRef{ID: oldest.RemoteRef, Key: outbox.KeyFor(hubID, hubName, seg, oldest.Class), Size: oldest.Bytes, Hash: oldest.RemoteHash}
	if hashes := d.Capabilities().Hashes; len(hashes) > 0 {
		ref.HashKind = hashes[0]
	}
	if _, err := d.Delete(ctx, ref); err != nil {
		return nil, err
	}
	deleted := *oldest
	deleted.State = catalog.PlacementDeleted
	if err := cat.UpdatePlacement(ctx, deleted, oldest.State, oldest.Bytes); err != nil {
		return nil, err
	}
	start := time.Now()
	var samples []quotaSample
	for {
		q, err := d.Quota(ctx)
		if err != nil {
			return samples, err
		}
		samples = append(samples, quotaSample{TMs: time.Since(start).Milliseconds(), Free: q.Free, InBin: q.InBin})
		if time.Since(start)+poll > readFor {
			return samples, nil
		}
		time.Sleep(poll)
	}
}
