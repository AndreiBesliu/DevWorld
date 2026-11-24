# Spike: skip the phone's waits, and find the hub from the phone (v0.1.3)

**Pe scurt pentru proprietar.** Pe 2026-11-24, în timpul probei pe Galaxy A54, ați cerut două lucruri pentru aplicația de test. De la v0.1.3:

- în rularea automată, cât timp aplicația așteaptă cele 10 minute fără încărcător dinaintea lui S3 sau pauza de răcire de după S3-9, apare butonul «Sari peste așteptare». Rezultatul scenariului care urmează notează că așteptarea a fost sărită, iar raportul spune asta lângă pragurile de temperatură și fps;
- pe telefon, aplicația caută singură spikehub în rețeaua Wi-Fi la care e conectat telefonul și arată adresele găsite ca butoane. O atingere completează adresa și verifică conexiunea. Dacă găsește un singur hub și câmpul e gol, îl completează singură.

**Goal:** Two owner requests of 2026-11-24 (thread "Rulează testul video pe Windows", relayed by the coordinator): a way past the phone's waits when the phone is already cold, and a list of the hubs the phone can reach instead of an address to type.

**Base:** `ipcam/phase0b-video-spike` after v0.1.2 (PRs #7 and #8).

## Design

### Skip the wait

- **Why it is safe.** The waits exist so that S3 starts from a cold phone (MV5, ruling B-R5 item 2). A phone that is still warm can only measure worse on heat, fps and CPU, never better, so a skipped wait cannot produce a false pass. A fail after a skipped wait has to be repeated with the whole wait.
- **The run** (`devicerun.ts`): `QueueEnv.offerSkip()` offers the skip at the start of each wait (the cold start before an S3, the cooling pause) and withdraws it when the wait ends by itself. A pressed skip ends the wait at once. The next scenario's result gets a note, on every attempt of that scenario: `wait skipped: off the charger for <s> s of 600 before <id>` or `wait skipped: cooling pause after <s> s of 600`. `QueueEnv.runScenario(id, notes)` passes the notes; the page appends them to the result before it is stored.
- **The page** (`ui.ts`): the button «Sari peste așteptare» with a line that says when to use it. The countdown updates its line in place instead of drawing the page again every second, so that a tap on the button is not lost to a redraw.
- **The report** (`spikereport`): MV3 and MV5 of a run with such a note carry a note that the wait was skipped, that a «Da» stands and that a «Nu» is to be repeated with the whole wait. The runs table lists the note as written, as for every note.

### Find the hub from the phone

- **The shell** (`MetricsPlugin.kt`, SPIKE-ONLY): `lanAddresses()` answers the private IPv4 addresses of the phone's active network with their prefix length (`ConnectivityManager.getLinkProperties`; `ACCESS_NETWORK_STATE`, which the app already has). The page never stores or uploads them (PH0-22).
- **The page** (`hubfinder.ts`): for each address, the hosts of its /24 (or of its smaller subnet), without the phone itself; each is asked for `GET /spike/v1/progress` on port 7080 with a 1.5 s timeout, 32 at a time, so that a /24 takes about 12 s at most. A host that answers as spikehub (a progress object) is a hub. spikehub already allows the app's origin (CORS, and Private Network Access).
- **The page** (`ui.ts`): on Android, the button «Caută hub-ul în rețea» under the hub address, the search's status and one button per hub found (`http://<address>:7080`, with spikehub's release). A tap fills the address and checks the connection. At launch, with an empty address field, the search starts by itself, and a single hub found fills the field.
- **The PC** is unchanged: the browsers use the page's own origin and ipcam-desktop fills `http://127.0.0.1:7080` (v0.1.2).
- **The product** already plans its own discovery (mDNS `_ipcam-hub._tcp`, spec §6.6 and the `local-network` plugin); this scan is SPIKE-ONLY and does not carry over.

## Tasks

- [x] `devicerun.ts`: `offerSkip`, the skipped wait's note on the next scenario; tests.
- [x] `ui.ts`, `i18n.ts`, `index.html`: the skip button, the countdown updated in place; tests.
- [x] `spikereport`: the note at MV3 and MV5; tests.
- [x] `MetricsPlugin.kt`, `android-metrics/src/index.ts`, `native.ts`, `native-android.ts`: `lanAddresses`; Kotlin and TypeScript tests.
- [x] `hubfinder.ts`: the hosts to ask and the search; tests.
- [x] `ui.ts`, `i18n.ts`: the search button, the list, the search at launch; tests.
- [x] Release `spike-video-v0.1.3` (`deviceplan.json` and the tests that pin it); the guide's step 5.
