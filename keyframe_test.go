//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
)

// FFmpeg 9 removed AVFrame.key_frame (AV_FRAME_FLAG_KEY in AVFrame.flags
// replaces it); a hard-coded key_frame offset reads another field there. A
// clip with a keyframe every 5 frames must read as keyframe, then not,
// in that rhythm.
func TestKeyFramesReadAsKeyFrames(t *testing.T) {
	if !requireFFmpeg(t) {
		t.Skip("FFmpeg not available")
	}
	clip := filepath.Join(t.TempDir(), "gop5.mkv")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=128x96:rate=25", "-frames:v", "10",
		"-c:v", "libx264", "-g", "5", "-keyint_min", "5", "-sc_threshold", "0", "-bf", "0", clip).CombinedOutput(); err != nil {
		t.Skipf("cannot make the clip: %v %s", err, out)
	}
	dec, err := NewDecoder(clip)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var got []bool
	for {
		f, err := dec.ReadFrame()
		if errors.Is(err, io.EOF) || (err == nil && f == nil) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if f.MediaType() != MediaTypeVideo {
			continue
		}
		got = append(got, f.IsKeyFrame())
	}
	if len(got) < 6 {
		t.Fatalf("decoded %d frames", len(got))
	}
	for i, key := range got {
		if want := i%5 == 0; key != want {
			t.Fatalf("frame %d: IsKeyFrame %v, want %v (all: %v)", i, key, want, got)
		}
	}
}
