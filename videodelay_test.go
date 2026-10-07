//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"strconv"
	"strings"
	"testing"
)

// codecpar's video_delay is the decoder's reorder depth; ffprobe prints it as
// has_b_frames (avcodec_parameters_to_context copies one into the other).
func TestVideoDelayIsWhatFFprobeCallsHasBFrames(t *testing.T) {
	requireShim(t)
	path := gopClip(t, "mkv")
	want := strings.TrimSpace(run(t, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=has_b_frames", "-of", "csv=p=0", path))
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, si := range d.Streams() {
		if si.Type != MediaTypeVideo {
			continue
		}
		if got := strconv.Itoa(si.VideoDelay); got != want || si.VideoDelay == 0 {
			t.Fatalf("VideoDelay = %s, ffprobe has_b_frames = %s (the clip has B-frames, so it is positive)", got, want)
		}
		return
	}
	t.Fatal("no video stream")
}
