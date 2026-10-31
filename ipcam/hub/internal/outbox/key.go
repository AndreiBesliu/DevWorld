package outbox

import (
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// KeyFor is the object key of s's copy in class (§9.10). An encrypted copy
// is named by opaque IDs only,
// e/<hubID>/<cam>/<YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>_<kind>_<profile>.mp4.age,
// and a clear one by readable names,
// <hubName>/<cam>/<YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>_<kind>_<profile>.mp4. The
// times are UTC, from s's start; <kind> is eveniment for an event clip, cont
// for continuous recording, and the class in lower case otherwise. The
// characters that Windows and SMB refuse in a name are removed, and the hub
// and camera folders are names that they accept (segment).
func KeyFor(hubID, hubName string, s catalog.Segment, class string) storage.ObjectKey {
	start := time.UnixMilli(s.StartMs).UTC()
	name := start.Format("20060102T150405Z") + "_" + keyKind(class) + "_" + clean(s.Profile) + ".mp4"
	root := []string{segment(hubName)}
	if s.Encrypted {
		root = []string{"e", segment(hubID)}
		name += ".age"
	}
	return storage.ObjectKey(strings.Join(append(root, segment(s.Cam), start.Format(time.DateOnly), name), "/"))
}

func keyKind(class string) string {
	switch class {
	case catalog.ClassEventClip:
		return "eveniment"
	case catalog.ClassContinuous:
		return "cont"
	}
	return strings.ToLower(class)
}

// clean removes from name the characters that Windows and SMB refuse in a
// file name: the control characters and <>:"/\|?*.
func clean(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, name)
}

// segment is name cleaned for a folder of a key: without the trailing dots
// and spaces that Windows drops; "_" when nothing is left, as of "" or "..";
// and with "_" after the base of a device name that Windows reserves, with or
// without an extension: CON_, com1_.txt.
func segment(name string) string {
	name = strings.TrimRight(clean(name), ". ")
	if name == "" {
		return "_"
	}
	base, _, _ := strings.Cut(name, ".")
	if reserved(base) {
		return base + "_" + name[len(base):]
	}
	return name
}

// reserved reports whether base, a name's part before its first dot, is a
// device name that Windows reserves in any case: CON, PRN, AUX, NUL, COM1 to
// COM9 and LPT1 to LPT9. Spaces after it count for nothing.
func reserved(base string) bool {
	switch b := strings.ToUpper(strings.TrimRight(base, " ")); b {
	case "CON", "PRN", "AUX", "NUL":
		return true
	default:
		return len(b) == 4 && (strings.HasPrefix(b, "COM") || strings.HasPrefix(b, "LPT")) && b[3] >= '1' && b[3] <= '9'
	}
}
