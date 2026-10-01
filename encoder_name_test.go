//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// VideoEncoderConfig.EncoderName picks the encoder by name, so a caller can
// ask for hevc_nvenc, hevc_qsv or hevc_vaapi instead of the codec's default
// encoder. libx265 is in every GPL build, so it stands in for them here.
func TestEncoderNameSelectsTheEncoder(t *testing.T) {
	if !requireFFmpeg(t) {
		t.Skip("FFmpeg not available")
	}
	out := filepath.Join(t.TempDir(), "x265.mkv")
	enc, err := NewEncoderWithOptions(out, &EncoderOptions{Video: &VideoEncoderConfig{
		EncoderName: "libx265", Width: 160, Height: 120,
		FrameRate: NewRational(25, 1), PixelFormat: PixelFormatYUV420P,
	}})
	if err != nil {
		t.Fatalf("libx265 by name: %v", err)
	}
	frame := FrameAlloc()
	defer func() { _ = FrameFree(&frame) }()
	AVUtil.SetFrameWidth(frame, 160)
	AVUtil.SetFrameHeight(frame, 120)
	AVUtil.SetFrameFormat(frame, int32(PixelFormatYUV420P))
	if err := AVUtil.FrameGetBuffer(frame, 0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := AVUtil.FrameMakeWritable(frame); err != nil {
			t.Fatal(err)
		}
		fillTestFrame(frame, i, 160, 120)
		if err := enc.WriteVideoFrame(frame); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Skipf("no ffprobe: %v", err)
	}
	if strings.TrimSpace(string(got)) != "hevc" {
		t.Fatalf("encoded %q, want hevc", got)
	}
}

// A name the FFmpeg build does not have is an error naming it, never a nil
// encoder handed on to avcodec_alloc_context3.
func TestAnUnknownEncoderNameIsAnErrorNamingIt(t *testing.T) {
	if !requireFFmpeg(t) {
		t.Skip("FFmpeg not available")
	}
	_, err := NewEncoderWithOptions(filepath.Join(t.TempDir(), "x.mkv"), &EncoderOptions{
		Video: &VideoEncoderConfig{EncoderName: "hevc_nosuch", Width: 64, Height: 64, FrameRate: NewRational(25, 1)},
	})
	if !errors.Is(err, ErrEncoderNotFound) {
		t.Fatalf("got %v, want ErrEncoderNotFound", err)
	}
	if !strings.Contains(err.Error(), "hevc_nosuch") {
		t.Fatalf("error %v does not name the encoder", err)
	}
}
