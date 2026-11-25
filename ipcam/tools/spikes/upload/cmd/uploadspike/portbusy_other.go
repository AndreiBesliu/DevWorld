// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !windows

package main

import "syscall"

// errAddrInUse is the bind error of a port that another socket holds.
const errAddrInUse = syscall.EADDRINUSE
