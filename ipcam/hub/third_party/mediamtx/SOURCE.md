# MediaMTX: upstream source of this fork

This directory is MediaMTX (MIT, `LICENSE` here) vendored as the nested module `github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx`. It is outside `ipcam/go.work`: build, test, vuln-check and license-check it from this directory with `GOWORK=off`. The patches on top of upstream are listed in [`../PATCHES.md`](../PATCHES.md).

- Upstream module: `github.com/bluenviron/mediamtx`
- Tag: `v1.21.1`
- Commit: `048255986f7e04b859b4c4efe651448ec785ecd4`
- Module zip SHA-256: `384791bd7c4e90536e7b3266a4f6fac2d7e81052087843987218e26a15a86a74` (the zip that proxy.golang.org serves; `go mod download` checked it against sum.golang.org, `h1:g2E8OJ6tS4SS7ZArZ+S7UgRGFna0k/GHZNKU7poQB3Q=`)
- hls.min.js SHA-256: `a12e7ee1cd64a69dcdb314157e45dafcba705bfb0b1440b7935cb265d374423e` (`internal/servers/hls/hls.min.js`: `dist/hls.min.js` of hls.js v1.7.3, Apache-2.0, from its `release.zip` with the SHA-256 `abbda85f3e9b8325fc6b8dbf4fa612ea87144ebad93c2150594e64aad58ec529` that `internal/servers/hls/hlsjsdownloader/HASH` pins)
- VERSION: `v1.21.1` (`internal/core/VERSION`, without a line break, as upstream's `versiongetter` writes it)
- Command: `tools/scripts/vendor-mediamtx.sh v1.21.1`, run from `ipcam`, then `GOWORK=off go mod tidy` in this directory
- Dependency changes against upstream's `go.mod`: `golang.org/x/net` v0.60.0 instead of v0.59.0 (see `../PATCHES.md`)

The version follows the plan's 14-day rule (ruling C31): v1.21.1 was published on 2026-09-20; v1.21.2 (2026-10-08) becomes eligible on 2026-10-22, and Task 3 re-vendors it before any patch exists if it starts on or after that day.

`go test -run '^TestMTX0' .` checks this file against the tree (`mtx0_test.go`).

## Licenses that go-licenses does not report

`tools/scripts/check-licenses.sh -C hub/third_party/mediamtx ./...` checks the fork's Go dependencies. Three licenses stay outside its report, so the release notices name them explicitly (ruling G35):

- MediaMTX itself, MIT (`LICENSE` here): the check skips the fork's own packages by the ipcam module prefix.
- hls.js v1.7.3, Apache-2.0: it is embedded as `hls.min.js`, a JavaScript file, not a Go module.
- `github.com/benburkert/openpgp/aes/keywrap`, BSD-2-Clause in the header of its `keywrap.go` ("Copyright 2014 Matthew Endsley"): its module has no license file for go-licenses to classify, so `check-licenses.sh` skips it by name. The fork reaches it through `github.com/datarhei/gosrt/crypto` (SRT encryption).
