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
// characters that Windows and SMB refuse in a name are removed.
func KeyFor(hubID, hubName string, s catalog.Segment, class string) storage.ObjectKey {
	start := time.UnixMilli(s.StartMs).UTC()
	name := start.Format("20060102T150405Z") + "_" + keyKind(class) + "_" + clean(s.Profile) + ".mp4"
	root := []string{clean(hubName)}
	if s.Encrypted {
		root = []string{"e", clean(hubID)}
		name += ".age"
	}
	return storage.ObjectKey(strings.Join(append(root, clean(s.Cam), start.Format(time.DateOnly), name), "/"))
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
