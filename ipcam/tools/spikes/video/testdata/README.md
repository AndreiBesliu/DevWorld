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
