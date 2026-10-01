//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/obinnaokechukwu/ffgo/internal/layout"
)

// ffmpegOrSkip skips the test when the ffmpeg and ffprobe executables that
// make and inspect its clips are not installed.
func ffmpegOrSkip(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("no %s executable to make or inspect test clips", bin)
		}
	}
	if err := Init(); err != nil {
		t.Skipf("FFmpeg libraries not available: %v", err)
	}
}

// requireShim skips the test unless the shim is loaded and matches the
// loaded FFmpeg release.
func requireShim(t *testing.T) {
	t.Helper()
	if err := Init(); err != nil {
		t.Skipf("FFmpeg libraries not available: %v", err)
	}
	if !layout.ShimLoaded() {
		t.Skip("no shim matching the loaded FFmpeg release")
	}
}

// run runs a command and fails the test with its output when it fails.
func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// ffprobeFrameCount is the number of frames ffprobe decodes from stream index.
func ffprobeFrameCount(t *testing.T, path string, index int) int64 {
	t.Helper()
	out := run(t, "ffprobe", "-v", "error", "-count_frames", "-select_streams", strconv.Itoa(index),
		"-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path)
	n, err := strconv.ParseInt(strings.TrimSpace(strings.Split(out, "\n")[0]), 10, 64)
	if err != nil {
		t.Fatalf("ffprobe frame count %q: %v", out, err)
	}
	return n
}

// cudaOrSkip opens the CUDA device, skipping the test without one.
func cudaOrSkip(t *testing.T) *HWDevice {
	t.Helper()
	if err := Init(); err != nil {
		t.Skipf("FFmpeg libraries not available: %v", err)
	}
	dev, err := NewHWDevice(HWDeviceTypeCUDA, "")
	if err != nil {
		t.Skipf("no CUDA device: %v", err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	return dev
}

// h264Clip is an 8-bit 256x144 H.264 clip of frames frames at 24 fps.
func h264Clip(t *testing.T, frames int) string {
	t.Helper()
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "h264.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=256x144:rate=24", "-frames:v", strconv.Itoa(frames),
		"-pix_fmt", "yuv420p", "-c:v", "libx264", "-preset", "veryfast", path)
	return path
}
