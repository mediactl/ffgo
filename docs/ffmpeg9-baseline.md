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
