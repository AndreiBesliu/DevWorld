package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// sentinelCamera is a camera whose address, user and password are sentinels
// assembled at run time (P46).
func sentinelCamera() CameraRef {
	return CameraRef{
		ID:    "real-1",
		XAddr: "http://192.0.2.77:8000/onvif/device_service",
		User:  "SENT" + "INEL-user",
		Pass:  "SENT" + "INEL-pass",
	}
}

// TestCameraRef_RedactsEverywhere (HUB-11, PH0-22): %v, %+v, %#v,
// json.Marshal and slog show the camera's ID, never its address, user or
// password, whether the value is printed alone, through a pointer or as a
// field.
func TestCameraRef_RedactsEverywhere(t *testing.T) {
	cam := sentinelCamera()
	type holder struct {
		Cam CameraRef
		Ptr *CameraRef
	}
	var outs []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		outs = append(outs, fmt.Sprintf(verb, cam), fmt.Sprintf(verb, &cam), fmt.Sprintf(verb, holder{cam, &cam}))
	}
	b, err := json.Marshal(cam)
	if err != nil {
		t.Fatal(err)
	}
	outs = append(outs, string(b))
	if b, err = json.Marshal(holder{cam, &cam}); err != nil {
		t.Fatal(err)
	}
	outs = append(outs, string(b))
	var logs bytes.Buffer
	for _, h := range []slog.Handler{slog.NewTextHandler(&logs, nil), slog.NewJSONHandler(&logs, nil)} {
		slog.New(h).Info("camera", "cam", cam, "ptr", &cam)
	}
	outs = append(outs, logs.String())
	for _, out := range outs {
		for _, secret := range []string{"192.0.2.77", "8000", "device_service", cam.User, cam.Pass, "SENT" + "INEL"} {
			if strings.Contains(out, secret) {
				t.Errorf("output %q holds %q", out, secret)
			}
		}
		if !strings.Contains(out, "real-1") {
			t.Errorf("output %q does not name the camera", out)
		}
	}
}

// TestCameraRef_ErrorsNameNoAddress: an unreachable camera's error names no
// address, port or URL, since a net/http error quotes the URL it failed on.
func TestCameraRef_ErrorsNameNoAddress(t *testing.T) {
	cam := sentinelCamera()
	cam.XAddr = closedXAddr(t)
	_, err := Probe(t.Context(), cam, Options{})
	if err == nil {
		t.Fatal("Probe on a closed port succeeded")
	}
	if got := Reason(err); got != ReasonOnvifPort {
		t.Errorf("Reason = %q; want %q", got, ReasonOnvifPort)
	}
	for _, secret := range []string{"127.0.0.1", hostPort(t, cam.XAddr), "http://", cam.User, cam.Pass} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error %q holds %q", err, secret)
		}
	}
}
