//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"strconv"
	"strings"
	"testing"
)

// skip_frame=nokey is libavcodec's AVDISCARD_NONKEY, which ffmpeg(1)'s
// -skip_frame nokey sets: the decoder outputs keyframes only.
func TestStreamDecoderOptionsReachTheCodecContext(t *testing.T) {
	path := gopClip(t, "mkv")
	want := strings.TrimSpace(run(t, "ffprobe", "-v", "error", "-select_streams", "v:0", "-skip_frame", "nokey",
		"-count_frames", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", path))
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	sd, err := d.NewStreamDecoder(d.VideoStream().Index, &StreamDecoderConfig{Options: map[string]string{
		"skip_frame": "nokey", "threads": "auto", "flags": "+unaligned",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer sd.Close()
	n := 0
	decodeStream(t, d, sd, func(Frame) { n++ })
	if strconv.Itoa(n) != want || n != 3 {
		t.Fatalf("decoded %d frames with skip_frame=nokey; ffprobe decodes %s, and the clip has 3 keyframes", n, want)
	}
}

func TestAnUnknownStreamDecoderOptionIsAnError(t *testing.T) {
	d, err := NewDecoder(gopClip(t, "mkv"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_, err = d.NewStreamDecoder(d.VideoStream().Index, &StreamDecoderConfig{Options: map[string]string{"no_such_option": "1"}})
	if err == nil || !strings.Contains(err.Error(), "no_such_option") {
		t.Fatalf("err = %v, want an error naming no_such_option", err)
	}
}
