# ffgo on FFmpeg 9.0: baseline

Host: the clustarr owner's workstation (Arch Linux, 2026-09-30) with FFmpeg
9.0.1 as shared libraries (libavutil 61, libavcodec 63, libavformat 63,
libavfilter 12, libswscale 10, libswresample 7) and FFmpeg 4.4's libraries
beside them (libavutil 56, libavcodec 58, libavformat 58, libswscale 5).
The shim was rebuilt against FFmpeg 9.0.1's headers
(`shim/prebuilt/linux-amd64/libffshim.so`; `ffshim_avcodec_version`
reports 63.1.101).

## Result of `CGO_ENABLED=0 go test ./... -count=1` on upstream `8e9c9c1`

| Package | Result |
| --- | --- |
| `internal/bindings`, `internal/handles`, `internal/platform`, `internal/shim`, `avfilter` | ok |
| `ffgo`, `avcodec`, `avformat`, `avutil`, `swscale` | panic before any test runs |

All five panics are the same:

```
panic: /usr/lib/libavutil.so.56: undefined symbol: av_channel_layout_default
```

The loader tries libavutil 59-56 and libavcodec/libavformat 61-58 only
(`internal/bindings/bindings.go`, `doLoad`), so it never reaches FFmpeg 9's
libavutil 61. It finds FFmpeg 4.4's `libavutil.so.56` instead, and the
bindings then ask it for `av_channel_layout_default`, which FFmpeg added in
5.1. On a host with only FFmpeg 9 the versioned lookups all miss and the
unversioned `libavutil.so` (9.0) is loaded beside whatever else matches, the
mix the loader must never produce.

Next: load one release's libraries, newest first (Task 2), then re-run.

## Result after the fixes (2026-09-30)

`CGO_ENABLED=0 go test ./... -count=1` on FFmpeg 9.0.1 (this host): **238
passed, 0 skipped, 0 failed**. The same suite against BtbN's FFmpeg 9.0.2
shared build, with the shim built from its headers (`ffmpeg9.yml`'s job,
run locally): all packages ok.

What it took:

1. **Load one release, newest first** (`internal/bindings/versions.go`).
   The loader knew FFmpeg 4-7 only and fell back to unversioned names; it
   now loads every library of one release (avutil, avcodec, avformat, and
   avfilter, swscale, swresample, avdevice when asked) or none, and
   `FFGO_FFMPEG_MAJOR` pins a release.
2. **Struct offsets from the shim** (`internal/layout`, `ffshim_offsetof`).
   On FFmpeg 9, 35 of the 111 offsets the Go code reads differ from the
   headers, among them `AVCodecContext.hw_frames_ctx` (Go 840, headers
   552) and most of `AVFormatContext`. The shim's values are used when it
   was built against the loaded release; `Init` refuses a release newer
   than 7 without one.
3. **`AVFrame.key_frame` is gone in FFmpeg 9.** Every frame read as a
   keyframe; `GetFrameKeyFrame` now reads `AV_FRAME_FLAG_KEY` from
   `AVFrame.flags` there. `AVCodec.name` was read at offset 8
   (`long_name`) on every release.
4. **Pixel formats by name.** `p010le`, `yuv420p10le` and `cuda` move
   between majors (161/64/119 in 4.4, 158/62/117 in 9.0) and are looked up
   with `av_get_pix_fmt`. `RGB48BE/LE` were wrong on every release;
   `RGBA64BE/LE` move and are deprecated in favour of the lookup.
5. **A shim is only used with its own release.** A shim links the FFmpeg
   libraries it was built against; `internal/shim` refuses one whose
   `DT_NEEDED` sonames name another release (upstream's prebuilt links
   FFmpeg 6's, this fork's 9's).

FFmpeg 4.x does not load in upstream ffgo either: `avutil` binds the
channel-layout API that FFmpeg added in 5.1. The fork leaves 4.x as
upstream has it. CI: `ci.yml` (Ubuntu's FFmpeg, upstream's job) and
`ffmpeg9.yml` (FFmpeg 9.0 shared).

## After Phase 1 (2026-10-01)

`go test ./... -count=1 -v` on FFmpeg 9.0.1 (this host, RTX 2070 Max-Q):
**268 passed, 0 skipped, 0 failed**, the GPU tests included. Phase 1 added
the stream decoder, packet-level encoders, layout-named resampling, side
data, GPU frame pools, filter-graph devices and the multi-stream muxer,
and found that `AVCodecID` values move between releases as pixel formats
do (HEVC 173 → 172 in FFmpeg 9; VP9, AV1, WebP and others moved too);
ffgo now resolves its constants by name.
