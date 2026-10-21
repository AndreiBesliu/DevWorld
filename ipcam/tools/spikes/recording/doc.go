// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package recording is the root of the Phase 0 (c1) recorder spike module.
//
// The spike measures the hub's recorder on the owner's devices and in CI; see
// ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md. It reaches
// production code only by running the ipcam-hub binary. Every source file
// carries the marker above, and the module is deleted after the report.
package recording
