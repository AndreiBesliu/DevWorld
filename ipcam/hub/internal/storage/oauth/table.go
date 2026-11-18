package oauth

import (
	"fmt"
	"strconv"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// The redirect URIs of the table (T34, P36). Desktop's and Entra's Windows
// URIs take the app's loopback port; Entra's web URI is uploadspike's
// handoff port, 7091.
const (
	redirectGoogleAndroid = "ro.ipcam.spike:/oauth2redirect"
	redirectEntraAndroid  = "ro.ipcam.spike://oauth/callback"
	redirectEntraWeb      = "http://localhost:7091/oauth/callback"
)

// RouteFor is the client type and the exact redirect URI of provider p on
// platform pl under plan ("" or "A", or "B1"), loopbackPort being the
// Windows app's (T34, FLW-31, P36, P39):
//
//	GOOGLE_DRIVE      android  Android (no secret)  ro.ipcam.spike:/oauth2redirect
//	GOOGLE_DRIVE      windows  Desktop (secret)     http://127.0.0.1:<port>/
//	GOOGLE_DRIVE      web      none: always B1, the TV client (secret)
//	ONEDRIVE_PERSONAL android  Entra public         ro.ipcam.spike://oauth/callback
//	ONEDRIVE_PERSONAL windows  Entra public         http://localhost:<port>/oauth/callback
//	ONEDRIVE_PERSONAL web      Entra public         http://localhost:7091/oauth/callback
//
// B1 is the hub's device flow on any pair: Google's TV client, or the same
// Entra client.
func RouteFor(p storage.Kind, pl Platform, plan string, loopbackPort int) (Route, error) {
	switch pl {
	case PlatformAndroid, PlatformWindows, PlatformWeb:
	default:
		return Route{}, fmt.Errorf("oauth: unknown platform %q", pl)
	}
	switch plan {
	case "", PlanA, PlanB1:
	default:
		return Route{}, fmt.Errorf("oauth: unknown plan %q", plan)
	}
	port := ""
	if pl == PlatformWindows && plan != PlanB1 {
		if loopbackPort < 1 || loopbackPort > 65535 {
			return Route{}, fmt.Errorf("oauth: a Windows login needs the app's loopback port, not %d", loopbackPort)
		}
		port = strconv.Itoa(loopbackPort)
	}
	switch p {
	case storage.KindGoogleDrive:
		switch {
		case plan == PlanB1, pl == PlatformWeb:
			return Route{ClientType: ClientTV, Plan: PlanB1}, nil
		case pl == PlatformAndroid:
			return Route{ClientType: ClientAndroid, RedirectURI: redirectGoogleAndroid, Plan: PlanA}, nil
		}
		return Route{ClientType: ClientDesktop, RedirectURI: "http://127.0.0.1:" + port + "/", Plan: PlanA}, nil
	case storage.KindOneDrivePersonal:
		switch {
		case plan == PlanB1:
			return Route{ClientType: ClientEntra, Plan: PlanB1}, nil
		case pl == PlatformAndroid:
			return Route{ClientType: ClientEntra, RedirectURI: redirectEntraAndroid, Plan: PlanA}, nil
		case pl == PlatformWeb:
			return Route{ClientType: ClientEntra, RedirectURI: redirectEntraWeb, Plan: PlanA}, nil
		}
		return Route{ClientType: ClientEntra, RedirectURI: "http://localhost:" + port + "/oauth/callback", Plan: PlanA}, nil
	}
	return Route{}, fmt.Errorf("oauth: %s has no OAuth login", p)
}
