// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package install

import (
	"context"
	"os/user"
	"strconv"
)

// lookupUser is the hub's identity's uid and gid; tests replace it.
var lookupUser = func(name string) (uid, gid int, err error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, err
	}
	if uid, err = strconv.Atoi(u.Uid); err != nil {
		return 0, 0, err
	}
	gid, err = strconv.Atoi(u.Gid)
	return uid, gid, err
}

// EnsureUser creates the hub's system user unless it exists, and returns
// its uid and gid.
func EnsureUser(ctx context.Context, run Commander) (uid, gid int, err error) {
	if uid, gid, err = lookupUser(SpikeUser); err == nil {
		return uid, gid, nil
	}
	if err := RunAll(ctx, run, UserCommands()); err != nil {
		return 0, 0, err
	}
	return lookupUser(SpikeUser)
}
