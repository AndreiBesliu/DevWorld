// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package upload is the root of the Phase 0 (c2) upload spike module.
//
// The spike measures uploads during a WAN outage (MI5), OAuth on the hub
// (MI6) and ONVIF events (MI8) on the owner's devices and in CI; see
// ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md. It reaches
// production code only by running the ipcam-hub and fakeproviders binaries.
// Every source file carries the marker above, and the module is deleted after
// the report.
package upload
