//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
)

// gopClip is a 3 s, 25 fps H.264 clip (75 frames, an IDR every 25 frames
// and no other keyframe, B-frames, BT.709 limited range) with a 48 kHz AAC
// track, in a container of extension ext ("mkv" or "mp4").
func gopClip(t *testing.T, ext string) string {
	t.Helper()
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "gop."+ext)
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3:sample_rate=48000",
		"-map", "0", "-map", "1",
		"-c:v", "libx264", "-preset", "veryfast", "-bf", "3",
		"-x264-params", "keyint=25:min-keyint=25:scenecut=0", "-pix_fmt", "yuv420p",
		"-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
		"-c:a", "aac", path)
	return path
}

// decodeStream feeds sd every packet of its stream from d, calls each for
// every decoded frame (borrowed: valid until each returns), and drains the
// decoder at end of file.
func decodeStream(t *testing.T, d *Decoder, sd *StreamDecoder, each func(Frame)) {
	t.Helper()
	drain := func() {
		for {
			f, err := sd.Receive()
			if errors.Is(err, ErrAgain) || errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				t.Fatalf("receive: %v", err)
			}
			each(f)
		}
	}
	send := func(p *Packet) {
		for {
			err := sd.Send(p)
			if errors.Is(err, ErrAgain) {
				drain()
				continue
			}
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			return
		}
	}
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if p == nil {
			break
		}
		if p.StreamIndex() != sd.StreamIndex() {
			continue
		}
		send(p)
		drain()
	}
	send(nil)
	drain()
}
