# Spike media

Simulated camera media for the Phase 0 (b) video spike. The files are generated, never
committed: everything in this folder except this README is ignored by git.

## Generate

```sh
cd ipcam
go run ./tools/spikes/video/cmd/mediagen -out tools/spikes/video/media
```

The generator needs `ffmpeg` and `ffprobe` with libx264 and libx265 on the `PATH`
(`apt-get install ffmpeg`). They are a CI and developer tool only: they are never shipped
and never part of a release asset. A run takes about two minutes and writes about 350 MB.
CI runs it in the `media` job of `.github/workflows/ipcam-spikes.yml` and uploads the
result as the artifact `spike-video-media`.

## Files

All streams are Annex-B elementary streams with GOP = fps, no B-frames, and parameter sets
before every IDR. Each one is a loop that starts with an IDR and ends at a GOP boundary, so
it repeats seamlessly. Bitrates are averages over the whole file (two-pass encoding).

| File | Band `Camera` | Codec, profile@level | Size | fps | Loop | Bitrate |
|---|---|---|---|---|---|---|
| `sub.h264` | 1 | H.264 Main@3.1 | 640x360 | 12 | 60 s (720 frames) | 512 kbps |
| `main.h264` | 2 | H.264 Main@4.0 | 1920x1080 | 20 | 60 s (1200 frames) | 4 Mbps |
| `main-high51.h264` | 3 | H.264 High@5.1 | 3840x2160 | 20 | 20 s (400 frames) | 8 Mbps |
| `main-h265.h265` | 4 | H.265 Main@4.1 | 1920x1080 | 20 | 60 s (1200 frames) | 2 Mbps |
| `hls/` | - | the `main` stream, `-c copy` | 1920x1080 | 20 | 10 min VOD | 4 Mbps |
| `manifest.json` | - | what `ffprobe` measured in each file | | | | |

`hls/` is an HLS fMP4 VOD: `index.m3u8`, `init.mp4` and 150 segments of 4 s
(`seg-00000.m4s` ...). It is `main.h264` repeated ten times, so its band repeats every 60 s.

`manifest.json` holds one entry per stream (`name`, `file`, `camera`, `codec`, `profile`,
`level`, `width`, `height`, `fps`, `gop`, `bitrate_bps`, `frames`, `has_b_frames`) and an
`hls` entry (`playlist`, `segment_seconds`, `segments`, `duration_seconds`). The values are
measured, not copied from the generator's table.

## Frame content

Every frame has a moving gradient with noise, a large printed frame number in the middle,
and the band: two rows of 48 blocks along the top edge, 16 px blocks from 1080 lines up and
8 px blocks below. The layout is documented in `../band/band.go`, and the shared test vectors
are in `../band/vectors.json`.

- The band `Camera` field is the media **file** id from the table above, not the number of a
  simulated camera: the hub serves many simulated cameras from the same file.
- The band `Frame` field is the frame index inside the loop. The printed number is the same
  value.
- The band `Loop` field is **always 0**: a file holds one loop, so the pixels cannot count
  how many times the hub has replayed it. A reader that needs the replay count has to take
  it from the hub's own send log.
