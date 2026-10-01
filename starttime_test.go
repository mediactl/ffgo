//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDecoderReportsTheInputsStartTime(t *testing.T) {
	ffmpegOrSkip(t)
	for name, want := range map[string]time.Duration{"ts": 1400 * time.Millisecond, "mkv": 0} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "clip."+name)
			run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=25:duration=1", "-c:v", "mpeg2video", path)
			d, err := NewDecoder(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if got := d.StartTime(); got < want-50*time.Millisecond || got > want+50*time.Millisecond {
				t.Errorf("StartTime %v, want about %v", got, want)
			}
		})
	}
}
