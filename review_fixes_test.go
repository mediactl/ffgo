//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// Final review #1: a decoder that cannot take a packet until its output is
// read must say so (ErrAgain), not report success and drop the packet.
func TestStreamDecoderSendReportsAFullDecoder(t *testing.T) {
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "mjpeg.avi")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=25", "-frames:v", "50", "-c:v", "mjpeg", path)
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	vi := d.VideoStream().Index
	sd, err := d.NewStreamDecoder(vi, &StreamDecoderConfig{Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer sd.Close()
	decoded, sent := 0, 0
	drain := func() {
		for {
			_, err := sd.Receive()
			if errors.Is(err, ErrAgain) || errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			decoded++
		}
	}
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		if p.StreamIndex() != vi {
			continue
		}
		for {
			err := sd.Send(p)
			if errors.Is(err, ErrAgain) { // full: read its output, then send again
				drain()
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		sent++
		if sent%3 == 0 { // read output only every third packet
			drain()
		}
	}
	for {
		err := sd.Send(nil)
		if errors.Is(err, ErrAgain) { // it may still hold input: read it first
			drain()
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	drain()
	if decoded != sent {
		t.Errorf("sent %d packets, decoded %d frames", sent, decoded)
	}
}

// Final review #2 and #3: a codec whose ID equals some ffgo constant's value
// (PAM is 66 in FFmpeg 9, ffgo's CodecIDBMP) must decode as itself, and
// StreamInfo.Codec is the codec's name, not the decoder's.
func TestRawCodecIDsDecodeAsThemselves(t *testing.T) {
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "frame.pam") // read back through image2 as codec pam
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=25", "-frames:v", "1", path)
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := d.Streams()[0]
	if s.CodecName != "pam" || s.Codec != "pam" {
		t.Errorf("CodecName %q, Codec %q; want pam, pam", s.CodecName, s.Codec)
	}
	if _, err := d.DecodeVideo(); err != nil {
		t.Errorf("decoding PAM: %v", err)
	}
}

func TestStreamInfoCodecIsTheCodecNotTheDecoder(t *testing.T) {
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "mp3.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=duration=1", "-c:a", "libmp3lame", path)
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.Streams()[0].Codec; got != "mp3" {
		t.Errorf("Codec %q, want mp3 (the decoder is %q)", got, d.Streams()[0].CodecName)
	}
}

// Final review #4: the encoder makes its own frame types; the source's GOP
// (an I-frame every 12) must not be forced onto the output.
func TestVideoStreamEncoderChoosesItsOwnFrameTypes(t *testing.T) {
	ffmpegOrSkip(t)
	src := filepath.Join(t.TempDir(), "gop12.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=128x72:rate=24", "-frames:v", "96",
		"-c:v", "libx264", "-g", "12", "-bf", "2", "-pix_fmt", "yuv420p", src)
	out := hdr10Transcode(t, src, hdrRun{encoder: "libx265"})
	types := run(t, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "frame=pict_type", "-of", "csv=p=0", out)
	if n := strings.Count(types, "I"); n >= 8 {
		t.Errorf("%d I-frames in 96: the source's GOP of 12 was forced onto the output", n)
	}
}

// Final review #5: an input frame that does not match the encoder is an
// error, not a crash.
func TestAudioEncoderRejectsAFrameOfAnotherLayout(t *testing.T) {
	requireShim(t)
	enc, err := NewAudioEncoder(AudioEncoderConfig2{SampleRate: 48000, Layout: "5.1", BitRate: 384000})
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	for name, f := range map[string]Frame{
		"stereo":   silentFrame(t, 48000, "stereo", 1024),
		"s16":      mustAudioFrame(t, SampleFormatS16, 48000, "5.1", 1024),
		"44.1 kHz": silentFrame(t, 44100, "5.1", 1024),
	} {
		err := enc.Encode(f, func(*Packet) error { return nil })
		_ = f.Free()
		if err == nil {
			t.Errorf("a %s frame was accepted by a 48 kHz fltp 5.1 encoder", name)
		}
	}
}

func mustAudioFrame(t *testing.T, fmt_ SampleFormat, rate int, layout string, n int) Frame {
	t.Helper()
	f, err := NewAudioFrame(fmt_, rate, layout, n)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Final review #6: a gap in the input's timestamps (dropped packets) must
// not shift every later AAC frame earlier by the gap.
func TestAACKeepsSyncAcrossAGap(t *testing.T) {
	requireShim(t)
	enc, err := NewAudioEncoder(AudioEncoderConfig2{SampleRate: 48000, Layout: "stereo", BitRate: 128000})
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	var last int64
	emit := func(p *Packet) error { last = p.PTS(); return nil }
	pts := int64(48000) // starts at 1 s
	for i := 0; i < 20; i++ {
		if i == 10 {
			pts += 24000 // half a second of audio missing
		}
		f := silentFrame(t, 48000, "stereo", 1024)
		f.SetPTS(pts)
		pts += 1024
		if err := enc.Encode(f, emit); err != nil {
			t.Fatal(err)
		}
		_ = f.Free()
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	// The last frame's samples start 1024 before the input's end.
	if want := pts - 1024; last < want-1024 || last > want+1024 {
		t.Errorf("last packet PTS %d, want about %d: the gap was lost", last, want)
	}
}

// Final review #7: stream side data the loaded release cannot hold is an
// error, not "absent"; on FFmpeg 9 a missing entry is (nil, false, nil).
func TestStreamSideDataReportsAbsenceWithoutAnError(t *testing.T) {
	requireShim(t)
	d, err := NewDecoder(h264Clip(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	b, ok, err := StreamSideData(d.VideoStream().CodecParameters(), PacketSideMasteringDisplay())
	if errors.Is(err, ErrNotSupported) {
		t.Skip("stream side data needs FFmpeg 6.1")
	}
	if err != nil || ok || b != nil {
		t.Errorf("got %v, %v, %v; want nil, false, nil", b, ok, err)
	}
}

// Plan Task 4's Frame.ChannelLayout.
func TestFrameChannelLayout(t *testing.T) {
	requireShim(t)
	f := silentFrame(t, 48000, "5.1(side)", 16)
	defer f.Free()
	if got := f.ChannelLayout(); got != "5.1(side)" {
		t.Errorf("ChannelLayout %q, want 5.1(side)", got)
	}
}

// FFmpeg 5.1 drops a packet the decoder still buffers when the flush packet
// arrives (its bitstream filter marks end of stream first), so Send(nil)
// refuses with ErrAgain until a Receive has said the decoder wants input.
func TestStreamDecoderRefusesAFlushWhileItMayHoldInput(t *testing.T) {
	d, err := NewDecoder(h264Clip(t, 4))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	vi := d.VideoStream().Index
	sd, err := d.NewStreamDecoder(vi, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sd.Close()
	for {
		p, err := d.ReadPacket()
		if err != nil || p == nil {
			t.Fatalf("no video packet: %v", err)
		}
		if p.StreamIndex() == vi {
			if err := sd.Send(p); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := sd.Send(nil); !errors.Is(err, ErrAgain) {
		t.Fatalf("flush right after a packet: %v, want ErrAgain", err)
	}
	for {
		if _, err := sd.Receive(); err != nil {
			if !errors.Is(err, ErrAgain) {
				t.Fatal(err)
			}
			break
		}
	}
	if err := sd.Send(nil); err != nil {
		t.Errorf("flush after the decoder asked for input: %v", err)
	}
}
