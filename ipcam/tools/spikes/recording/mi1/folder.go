// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

// DataFolders are the folders of the release folder that the probe's
// programs write: results/, which is published, and work/, which is
// private. Everything else in the folder is the release, which the boot
// task, the real cuts' shortcut and the bugcheck run elevated, so that
// only an administrator may change it (security-1).
var DataFolders = []string{"results", "work"}

// The well-known SIDs in the PC's release folder's DACL.
const (
	administratorsSID = "S-1-5-32-544"
	systemSID         = "S-1-5-18"
	ownerRightsSID    = "S-1-3-4"
)

// ProtectFolderCommands are the icacls commands that give the PC's release
// folder dir a protected DACL, without the inheritance that lets every
// authenticated user change a folder in the root of C: (security-1).
// Administrators and SYSTEM have Full control, and the owner's user,
// userSID, Read & Execute, and Modify on the data folders, which must
// exist. OWNER RIGHTS has Read & Execute: the owner's user extracted the
// release and owns its files, and an owner may otherwise always change a
// file's DACL. Windows passes the folder's entries on to everything in it.
func ProtectFolderCommands(dir, userSID string) [][]string {
	cmds := [][]string{{"icacls", dir, "/inheritance:r", "/grant:r",
		"*" + administratorsSID + ":(OI)(CI)F", "*" + systemSID + ":(OI)(CI)F",
		"*" + userSID + ":(OI)(CI)RX", "*" + ownerRightsSID + ":(OI)(CI)RX"}}
	for _, name := range DataFolders {
		cmds = append(cmds, []string{"icacls", DataFolder(dir, name), "/grant:r", "*" + userSID + ":(OI)(CI)M"})
	}
	return cmds
}

// UnprotectFolderCommands give the PC's release folder dir, and everything
// in it, back the permissions that they inherit, as when the owner
// extracted it: the owner can then delete it (security-1).
func UnprotectFolderCommands(dir string) [][]string {
	return [][]string{{"icacls", dir, "/reset", "/T", "/C", "/Q"}}
}

// DataFolder is the data folder name of the PC's release folder dir.
func DataFolder(dir, name string) string { return dir + `\` + name }

// AdministratorsOwned reports whether owner, the SID of a file's owner, is
// Administrators or SYSTEM: no other account wrote the file.
func AdministratorsOwned(owner string) bool {
	return owner == administratorsSID || owner == systemSID
}
