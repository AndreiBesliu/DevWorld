package storage_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestUploadState_NeverPrintsSession (PH0-22): the provider's session is a
// bearer secret, so fmt and log/slog print an UploadState without it, as a
// value or through a pointer. Its JSON, which goes to the catalog only, keeps
// it.
func TestUploadState_NeverPrintsSession(t *testing.T) {
	session := "SENT" + "INEL-upload-session"
	st := storage.UploadState{Key: "cam-1/seg.mp4", Size: 10, Session: session, Offset: 4}

	printed := map[string]string{}
	for _, verb := range []string{"%v", "%+v", "%s", "%#v"} {
		printed[verb] = fmt.Sprintf(verb, st)
		printed[verb+" of a pointer"] = fmt.Sprintf(verb, &st)
	}
	for name, handler := range map[string]func(*bytes.Buffer) slog.Handler{
		"slog text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"slog JSON": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var b bytes.Buffer
		slog.New(handler(&b)).Info("upload", "state", st, "pointer", &st)
		printed[name] = b.String()
	}
	for name, out := range printed {
		if strings.Contains(out, session) || !strings.Contains(out, "cam-1/seg.mp4") {
			t.Errorf("%s = %s; want the key without the session", name, out)
		}
	}

	raw, err := json.Marshal(st)
	if err != nil || !strings.Contains(string(raw), `"Session":"`+session+`"`) {
		t.Errorf("JSON = %s, %v; want the session in it", raw, err)
	}
}
