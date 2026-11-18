package oauth

import (
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestOAuth_RouteTable (T34, FLW-31, P36, P39): the client type and the
// exact redirect URI of each provider × platform pair, plan A; B1, the
// hub's device flow, on any pair, with Google's TV client or the same Entra
// client; web × Google, which has no plan A, always B1.
func TestOAuth_RouteTable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider storage.Kind
		platform Platform
		plan     string
		port     int
		want     Route
	}{
		{"GOOGLE_DRIVE android", storage.KindGoogleDrive, PlatformAndroid, "", 0, Route{ClientAndroid, "ro.ipcam.spike:/oauth2redirect", PlanA}},
		{"GOOGLE_DRIVE windows", storage.KindGoogleDrive, PlatformWindows, "A", 53123, Route{ClientDesktop, "http://127.0.0.1:53123/", PlanA}},
		{"GOOGLE_DRIVE web", storage.KindGoogleDrive, PlatformWeb, "", 0, Route{ClientTV, "", PlanB1}},
		{"GOOGLE_DRIVE web asked for A", storage.KindGoogleDrive, PlatformWeb, "A", 53123, Route{ClientTV, "", PlanB1}},
		{"ONEDRIVE_PERSONAL android", storage.KindOneDrivePersonal, PlatformAndroid, "", 0, Route{ClientEntra, "ro.ipcam.spike://oauth/callback", PlanA}},
		{"ONEDRIVE_PERSONAL windows", storage.KindOneDrivePersonal, PlatformWindows, "", 53123, Route{ClientEntra, "http://localhost:53123/oauth/callback", PlanA}},
		{"ONEDRIVE_PERSONAL web", storage.KindOneDrivePersonal, PlatformWeb, "", 0, Route{ClientEntra, "http://localhost:7091/oauth/callback", PlanA}},
		{"GOOGLE_DRIVE android B1", storage.KindGoogleDrive, PlatformAndroid, "B1", 0, Route{ClientTV, "", PlanB1}},
		{"GOOGLE_DRIVE windows B1", storage.KindGoogleDrive, PlatformWindows, "B1", 0, Route{ClientTV, "", PlanB1}},
		{"ONEDRIVE_PERSONAL web B1", storage.KindOneDrivePersonal, PlatformWeb, "B1", 0, Route{ClientEntra, "", PlanB1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RouteFor(tc.provider, tc.platform, tc.plan, tc.port)
			if err != nil || got != tc.want {
				t.Errorf("RouteFor = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name     string
		provider storage.Kind
		platform Platform
		plan     string
		port     int
	}{
		{"windows without a loopback port", storage.KindGoogleDrive, PlatformWindows, "", 0},
		{"a port out of range", storage.KindOneDrivePersonal, PlatformWindows, "", 70000},
		{"an unknown platform", storage.KindGoogleDrive, "ios", "", 0},
		{"an unknown plan", storage.KindGoogleDrive, PlatformAndroid, "B2", 0},
		{"a provider without OAuth", storage.KindLocal, PlatformAndroid, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := RouteFor(tc.provider, tc.platform, tc.plan, tc.port); err == nil {
				t.Errorf("RouteFor = %+v, want an error", got)
			}
		})
	}
}
