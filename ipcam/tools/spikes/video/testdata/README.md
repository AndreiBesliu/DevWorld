# Test data shared by the video spike's packages

`results-v0.1.0/` is a results folder written by spike-video-v0.1.0's own
code (tag `spike-video-v0.1.0`, commit 55aa6ff): its spikehub, built with its
app embedded, ran with `-sims 4 -mdns=false` on the media of `cmd/mediagen`,
and its app ran by its autorun (`?autorun=<scenario>`) in Chrome for Testing
with a Windows user agent. The files are as the hub stored them: S10, S9, S1
(once interrupted by a hidden page, once complete) and S7. They hold no
address, URL, serial number or SSID (PH0-22).

v0.1.1 reads such a folder unchanged (post-release task A, requirement 9):
spikehub's progress (`cmd/spikehub/progress_test.go`), the report
(`cmd/spikereport/main_test.go`) and the app (`app/src/results.test.ts`).

`classes/` holds results files of every class that the report knows (usable,
interrupted, failed, on the charger, informative) and of every kind of device
key, and `classes.json` what each one counts for: its device key and its class.
spikehub's progress and the report decide both with the same Go package
(`resultrules`), and three tests read these fixtures against `classes.json`,
each through its own reading of the files: `resultrules`, spikehub's
`TestProgress_CountsTheRunsThatTheReportUses` and the report's
`TestClassify_TheSharedFixtures`. The automatic run then skips a scenario
exactly when the report can use its run. The files are synthetic: user agents,
models and version strings only (PH0-22).

`classes/android-s4-battery.json` is the phone's S4 as the automatic run of
v0.1.1 writes it, on battery from start to end (ruling B-R4; S4 reads no
charger state). The report's `TestMV7_ThePhonesS4OnBattery_IsJudged` judges
MV7 on it.
