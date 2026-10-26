// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

// labVolumes is where a Windows lab shard's runs record: plain folders.
// The image of each encrypted run is a Linux lab extra (G08, Images).
func labVolumes(_, root string) Volumes { return Dirs{Root: root} }
