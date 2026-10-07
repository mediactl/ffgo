//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"testing"

	"github.com/obinnaokechukwu/ffgo/avformat"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// ffmpeg(1) moves an -ss seek back by 3/23 s for a demuxer without
// AVFMT_SEEK_TO_PTS when a stream has a video delay: Matroska lacks the
// flag (0x0), mov/mp4 has it (0x4008008 on libavformat 63).
func TestSeekToPTSIsTheDemuxersFlag(t *testing.T) {
	requireShim(t)
	for ext, want := range map[string]bool{"mkv": false, "mp4": true} {
		d, err := NewDecoder(gopClip(t, ext))
		if err != nil {
			t.Fatal(err)
		}
		if got := d.SeekToPTS(); got != want {
			t.Errorf("%s: SeekToPTS() = %v, want %v", ext, got, want)
		}
		_ = d.Close()
	}
}

func TestSeekToPTSFlagMatchesTheHeaders(t *testing.T) {
	requireShim(t)
	if v, ok := shim.EnumValue("AVFMT_SEEK_TO_PTS"); !ok || v != avformat.AVFMT_SEEK_TO_PTS {
		t.Fatalf("AVFMT_SEEK_TO_PTS: headers %#x (known %v), ffgo %#x", v, ok, avformat.AVFMT_SEEK_TO_PTS)
	}
}
