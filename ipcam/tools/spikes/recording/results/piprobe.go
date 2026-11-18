// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package results

// PiProbeSet is the set of recspike-probe's marker for an armv6 Pi (G31,
// G32): an MI1 envelope that measures nothing.
const PiProbeSet = "pi-probe"

// PiProbeMarker is what recspike-probe writes on an armv6 Pi, which the
// spike cannot measure: an MI1 envelope of host "pi" in PiProbeSet, with
// the Pi's model as the probe read it (raw, its revision and the source,
// "cpuinfo" or "device-tree"), known and unsupported, and no run. Its run
// id is the set's first, so that running the probe again replaces it. The
// judges read any envelope of host "pi" with such a model as "the Pi's
// parts move to Faza 0-bis" (G31); G10 counts no run of it, and
// `reccontroller pull` then expects nothing with the Pi as host. atMs is
// when the probe ran.
func PiProbeMarker(raw, revision, source string, atMs int64) Envelope {
	return Envelope{
		Schema: Schema, Code: "MI1", RunID: PiProbeSet + "-1", AtMs: atMs, Set: PiProbeSet, Host: "pi", OS: "linux",
		PiModel: &PiModelInfo{Raw: raw, Revision: revision, Tier: "unsupported", Known: true, Source: source},
	}
}
