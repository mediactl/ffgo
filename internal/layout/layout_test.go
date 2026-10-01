//go:build !ios && !android && (amd64 || arm64)

package layout_test

import (
	"testing"

	"github.com/obinnaokechukwu/ffgo"
	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/layout"
)

// Every struct offset ffgo's Go code uses must name a field the shim,
// compiled against the running FFmpeg's headers, knows -- the shim's value
// is then the one read. On FFmpeg 9 the Go values (written for 6/7) drift;
// they are logged, and used only where no matching shim exists.
func TestEveryOffsetIsKnownToTheHeaders(t *testing.T) {
	if err := ffgo.Init(); err != nil {
		t.Skipf("no FFmpeg: %v", err)
	}
	if !layout.ShimLoaded() {
		t.Skip("no shim on this host: offsets cannot be checked")
	}
	if n := layout.Registered(); n < 100 {
		t.Fatalf("only %d offsets registered: some Go offsets bypass layout.Offset", n)
	}
	if m := layout.Mismatches(); len(m) > 0 {
		t.Fatalf("offsets the Go code reads that this FFmpeg's headers do not have:\n%v", m)
	}
	for _, d := range layout.Differences() {
		t.Log(d)
	}
}

// Pixel formats are an enum whose values can move between FFmpeg majors;
// every format ffgo names must be the library's value for that name.
func TestPixelFormatsComeFromTheLibrary(t *testing.T) {
	if err := ffgo.Init(); err != nil {
		t.Skipf("no FFmpeg: %v", err)
	}
	for name, want := range map[string]avutil.PixelFormat{
		"yuv420p": avutil.PixelFormatYUV420P, "yuyv422": avutil.PixelFormatYUYV422,
		"rgb24": avutil.PixelFormatRGB24, "bgr24": avutil.PixelFormatBGR24,
		"yuv422p": avutil.PixelFormatYUV422P, "yuv444p": avutil.PixelFormatYUV444P,
		"yuv410p": avutil.PixelFormatYUV410P, "yuv411p": avutil.PixelFormatYUV411P,
		"gray": avutil.PixelFormatGray8, "monow": avutil.PixelFormatMonoW,
		"monob": avutil.PixelFormatMonoB, "pal8": avutil.PixelFormatPAL8,
		"yuvj420p": avutil.PixelFormatYUVJ420P, "yuvj422p": avutil.PixelFormatYUVJ422P,
		"yuvj444p": avutil.PixelFormatYUVJ444P, "nv12": avutil.PixelFormatNV12,
		"nv21": avutil.PixelFormatNV21, "argb": avutil.PixelFormatARGB,
		"rgba": avutil.PixelFormatRGBA, "abgr": avutil.PixelFormatABGR,
		"bgra": avutil.PixelFormatBGRA, "gray16be": avutil.PixelFormatGray16BE,
		"gray16le": avutil.PixelFormatGray16LE, "rgb48be": avutil.PixelFormatRGB48BE,
		"rgb48le": avutil.PixelFormatRGB48LE, "rgba64be": avutil.PixelFormatRGBA64BE,
		"rgba64le": avutil.PixelFormatRGBA64LE,
		"p010le": avutil.PixelFormatP010LE(), "yuv420p10le": avutil.PixelFormatYUV420P10LE(),
		"cuda": avutil.PixelFormatCUDA(),
	} {
		if got := avutil.PixelFormatByName(name); got != want {
			t.Errorf("%s: library says %d, ffgo's value is %d", name, got, want)
		}
	}
	if avutil.PixelFormatByName("no-such-format") != avutil.PixelFormatNone {
		t.Error("an unknown name must be PixelFormatNone")
	}
}
