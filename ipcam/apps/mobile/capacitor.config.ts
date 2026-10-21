import type { CapacitorConfig } from '@capacitor/cli';

// The Android shell of Phase 0 carries only the video spike app; the product's
// app replaces it in Phase 4, without server.url (spec §6.7).
const config: CapacitorConfig = {
  // Temporary id of the Phase 0 test APK, never published (OPN-01).
  appId: 'ro.ipcam.spike',
  // The launcher label, Romanian like the app's start page (Proba video ipcam).
  appName: 'Proba ipcam',
  webDir: '../../tools/spikes/video/app/dist',
  server: {
    // SPIKE-ONLY (plan 2026-10-18-phase0b-video-spike, Task 10): the page's
    // origin is http://localhost, so it may call the spike hub over plain HTTP
    // on the LAN (port 7080) without mixed-content blocking. The app's network
    // security config allows that cleartext only in the `spike` flavour.
    // Capacitor reads this one config in every flavour, so the `production`
    // flavour gets the scheme too, although it forbids cleartext.
    // TODO (Phase 4): split the scheme per flavour: `http` in `spike` only,
    // and the default https://localhost of spec §11.5 in `production`.
    androidScheme: 'http',
  },
};

export default config;
