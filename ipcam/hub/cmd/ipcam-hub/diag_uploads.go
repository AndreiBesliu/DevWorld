package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// uploadsSchema names the output of diag uploads.
const uploadsSchema = "ipcam.diag.uploads/1"

// uploadsCounts is diag uploads' line of one destination. Missing,
// Duplicates and HashMismatches are null without --list-remote: only the
// listing measures them.
type uploadsCounts struct {
	Schema         string `json:"schema"`
	Dest           string `json:"dest"`
	Segments       int    `json:"segments"`
	Verified       int    `json:"verified"`
	Pending        int    `json:"pending"`
	Failed         int    `json:"failed"`
	Deleted        int    `json:"deleted"`
	Missing        *int   `json:"missing"`
	Duplicates     *int   `json:"duplicates"`
	HashMismatches *int   `json:"hashMismatches"`
	ListCalls      int    `json:"listCalls"`
}

// routedSegment is a complete segment that a route sends to a destination,
// and the key of its copy there.
type routedSegment struct {
	seg catalog.Segment
	key storage.ObjectKey
}

// callLogger is a driver that counts its API calls (Drive's CallLog).
type callLogger interface{ CallLog() storage.CallCounts }

// diagUploadsCommand is "ipcam-hub diag uploads --config <run.json>
// [--list-remote]" (OK14, MI5): one ipcam.diag.uploads/1 line per
// destination of the uploads section. segments counts the complete local
// segments that a route sends to the destination, its stream and its
// window as the callback matches them, but those whose placement is
// deleted (dest-probe --delete-segment made them), which deleted counts;
// verified, pending (uploading included) and failed count their placements.
// A segment outside every route's window is in no count. With --list-remote
// it lists each day folder of those segments' copies through List, the
// reconciliation path, never in the running hub's counters: duplicates are
// the keys that hold more than one object, missing the segments with no
// object, hashMismatches the objects whose hash differs from Digest of the
// local file; listCalls counts the listing's API calls, or its List calls
// for a driver that counts none. It opens the catalog that a run left and
// reads state/hub_id, creating neither (g2-11), and exits 1 on a usage
// error, a refusal or a failed listing.
func diagUploadsCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag uploads", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "the run configuration (run.json)")
	list := flags.Bool("list-remote", false, "list the destinations' copies (the reconciliation path)")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if *config == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	failed := func(err error) int {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag uploads:", err)
		return exitFatal
	}
	cfg, hubID, err := uploadsOf(*config)
	dataDir, uploads := cfg.DataDir, cfg.Uploads
	if err != nil {
		return failed(err)
	}
	ctx := context.Background()
	cat, err := openExistingCatalog(ctx, dataDir)
	if err != nil {
		return failed(err)
	}
	defer func() { _ = cat.Close() }()
	segs, err := cat.Segments(ctx)
	if err != nil {
		return failed(err)
	}
	placements, err := cat.Placements(ctx)
	if err != nil {
		return failed(err)
	}
	code := exitOK
	for _, def := range uploads.Destinations {
		counts, routed := countUploads(uploads, hubID, def.ID, segs, placements)
		if *list {
			d, err := diagDestination(ctx, cfg, cat, def, hubID)
			if err == nil {
				err = listRemote(ctx, d, filepath.Join(dataDir, "rec"), routed, &counts)
			}
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "ipcam-hub: diag uploads: %s: %v\n", def.ID, err)
				code = exitFatal
			}
		}
		if c := printJSONLine(stdout, stderr, counts); c != exitOK {
			return c
		}
	}
	return code
}

// countUploads counts destID's segments and placements from the catalog's
// rows, and returns its routed segments that are not deleted.
func countUploads(u *uploadsConfig, hubID, destID string, segs []catalog.Segment, placements []catalog.Placement) (uploadsCounts, []routedSegment) {
	counts := uploadsCounts{Schema: uploadsSchema, Dest: destID}
	byObject := map[string]*catalog.Placement{}
	for i, p := range placements {
		if p.DestinationID == destID {
			byObject[p.ObjectID] = &placements[i]
		}
	}
	var routed []routedSegment
	for _, s := range segs {
		if s.State != catalog.StateComplete {
			continue
		}
		class, ok := routeClass(u, s, destID)
		if !ok {
			continue
		}
		p := byObject[s.ID]
		if p != nil && p.State == catalog.PlacementDeleted {
			counts.Deleted++
			continue
		}
		counts.Segments++
		if p != nil {
			class = p.Class
			switch p.State {
			case catalog.PlacementVerified:
				counts.Verified++
			case catalog.PlacementPending, catalog.PlacementUploading:
				counts.Pending++
			case catalog.PlacementFailed:
				counts.Failed++
			}
		}
		routed = append(routed, routedSegment{seg: s, key: outbox.KeyFor(hubID, u.HubName, s, class)})
	}
	return counts, routed
}

// routeClass is the class of the first route that sends s to destID, and
// whether there is one.
func routeClass(u *uploadsConfig, s catalog.Segment, destID string) (string, bool) {
	for _, r := range u.Routes {
		if r.holds(s) && slices.Contains(r.Destinations, destID) {
			return r.Class, true
		}
	}
	return "", false
}

// listRemote lists d's day folders of routed's copies and fills the counts
// that the listing measures. An object whose listing has no hash, as the
// local disk's, gets it from Stat.
func listRemote(ctx context.Context, d storage.Destination, recDir string, routed []routedSegment, counts *uploadsCounts) error {
	var dirs []string
	for _, r := range routed {
		if dir := path.Dir(string(r.key)); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	slices.Sort(dirs)
	before := callTotal(d)
	objects := map[storage.ObjectKey][]storage.ObjectRef{}
	lists := 0
	for _, dir := range dirs {
		lists++
		for info, err := range d.List(ctx, storage.ObjectKey(dir+"/")) {
			if err != nil {
				return err
			}
			if path.Dir(string(info.Ref.Key)) == dir {
				objects[info.Ref.Key] = append(objects[info.Ref.Key], info.Ref)
			}
		}
	}
	if _, ok := d.(callLogger); ok {
		counts.ListCalls = callTotal(d) - before
	} else {
		counts.ListCalls = lists
	}
	missing, duplicates, mismatches := 0, 0, 0
	for _, refs := range objects {
		if len(refs) > 1 {
			duplicates++
		}
	}
	for _, r := range routed {
		refs := objects[r.key]
		if len(refs) == 0 {
			missing++
			continue
		}
		for _, ref := range refs {
			bad, err := hashDiffers(ctx, d, filepath.Join(recDir, filepath.FromSlash(r.seg.Path)), ref)
			if err != nil {
				return err
			}
			if bad {
				mismatches++
			}
		}
	}
	counts.Missing, counts.Duplicates, counts.HashMismatches = &missing, &duplicates, &mismatches
	return nil
}

// hashDiffers reports whether the object ref's hash differs from Digest of
// the local file in the same kind; one with no hash in its listing is read
// with Stat first.
func hashDiffers(ctx context.Context, d storage.Destination, file string, ref storage.ObjectRef) (bool, error) {
	if ref.HashKind == "" {
		info, err := d.Stat(ctx, ref)
		if err != nil {
			return false, err
		}
		ref = info.Ref
	}
	if ref.HashKind == "" {
		return false, nil // the provider gives no hash: nothing to compare
	}
	f, err := os.Open(file) //nolint:gosec // G304: a segment under rec/, from the catalog
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	digest, err := storage.Digest(ref.HashKind, f)
	if err != nil {
		return false, err
	}
	return digest != ref.Hash, nil
}

// callTotal is d's API calls so far, 0 for a driver that counts none.
func callTotal(d storage.Destination) int {
	logger, ok := d.(callLogger)
	if !ok {
		return 0
	}
	total := 0
	for _, n := range logger.CallLog() {
		total += n
	}
	return total
}
