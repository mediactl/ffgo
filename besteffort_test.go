//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"strconv"
	"strings"
	"testing"
)

func TestBestEffortTimestampIsFFprobes(t *testing.T) {
	requireShim(t)
	path := gopClip(t, "mkv")
	var want []string
	for _, line := range strings.Fields(run(t, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "frame=best_effort_timestamp", "-of", "csv=p=0", path)) {
		// A frame with side data (x264's SEI on the first) prints "0,": the
		// timestamp is the first field.
		want = append(want, strings.Split(line, ",")[0])
	}
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	sd, err := d.NewStreamDecoder(d.VideoStream().Index, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sd.Close() }()
	var got []string
	decodeStream(t, d, sd, func(f Frame) { got = append(got, strconv.FormatInt(f.BestEffortTimestamp(), 10)) })
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("best-effort timestamps:\nffgo    %v\nffprobe %v", got, want)
	}
}
