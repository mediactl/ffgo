//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/obinnaokechukwu/ffgo/avcodec"
)

// What cluster-plex's GPU pool needs to segment Plex's conversions in
// memory: frames forced to keyframes, a muxer drained to its writer at a
// cut, and a whole file muxed through a writer it can seek.

// x264Encoder is a 64x64 yuv420p libx264 encoder at 24 fps whose own GOP
// never ends inside a test (keyint 250, no scene cuts), so every keyframe
// past the first is one the test forced.
func x264Encoder(t *testing.T, globalHeader, keepTypes bool) *VideoStreamEncoder {
	t.Helper()
	enc, err := NewVideoStreamEncoder(VideoStreamEncoderConfig{
		VideoEncoderConfig: VideoEncoderConfig{
			EncoderName: "libx264", Width: 64, Height: 64, PixelFormat: PixelFormatYUV420P,
			FrameRate: NewRational(24, 1), GOPSize: 250,
			CodecOptions: map[string]string{
				"x264-params": "keyint=250:min-keyint=250:scenecut=0:log-level=error",
				"forced-idr":  "1",
			},
		},
		TimeBase:       NewRational(1, 24),
		GlobalHeader:   globalHeader,
		KeepFrameTypes: keepTypes,
	})
	if errors.Is(err, ErrEncoderNotFound) {
		t.Skip("no libx264 in this FFmpeg")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = enc.Close() })
	return enc
}

func isKey(p *Packet) bool { return avcodec.GetPacketFlags(p.ptr)&avcodec.PacketFlagKey != 0 }

func TestAForcedFrameEncodesAsAKeyframe(t *testing.T) {
	requireShim(t)
	enc := x264Encoder(t, false, true)
	var keys []int64
	emit := func(p *Packet) error {
		if isKey(p) {
			keys = append(keys, p.PTS())
		}
		return nil
	}
	for i := int64(0); i < 30; i++ {
		f := grayFrame(t, 64, 64, PixelFormatYUV420P)
		f.SetPTS(i)
		if i == 12 {
			f.SetPictureType(PictureTypeI)
		}
		if err := enc.Encode(f, emit); err != nil {
			t.Fatal(err)
		}
		_ = f.Free()
	}
	if err := enc.Flush(emit); err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []int64{0, 12}) {
		t.Errorf("keyframes at %v, want [0 12]", keys)
	}
}

// tsPackets is how many packets an MPEG-TS stream holds, of every stream:
// the interleaving queue holds whichever stream runs ahead (here the audio,
// as libx264's lookahead delays the video).
func tsPackets(t *testing.T, ts []byte) int {
	t.Helper()
	d, err := NewDecoderFromReader(bytes.NewReader(ts), "mpegts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	n := 0
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			return n
		}
		n++
	}
}

func TestDrainHandsEveryWrittenPacketToTheWriter(t *testing.T) {
	requireShim(t)
	var out bytes.Buffer
	m, err := NewMuxerToWriter(&out, "mpegts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	venc := x264Encoder(t, false, false)
	aenc, err := NewAudioEncoder(AudioEncoderConfig2{
		SampleRate: 48000, Layout: "stereo", BitRate: 128000, InputTimeBase: NewRational(1, 48000),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = aenc.Close() }()
	vs, err := m.AddEncoderStream(venc, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	as, err := m.AddEncoderStream(aenc, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	written, frames := 0, 0
	video := func(p *Packet) error { written++; frames++; return m.WritePacket(vs, p) }
	audio := func(p *Packet) error { written++; return m.WritePacket(as, p) }
	feed := func(from, to int64) {
		for i := from; i < to; i++ {
			f := grayFrame(t, 64, 64, PixelFormatYUV420P)
			f.SetPTS(i)
			if err := venc.Encode(f, video); err != nil {
				t.Fatal(err)
			}
			_ = f.Free()
			a := silentFrame(t, 48000, "stereo", 2000) // one video frame's worth
			a.SetPTS(i * 2000)
			if err := aenc.Encode(a, audio); err != nil {
				t.Fatal(err)
			}
			_ = a.Free()
		}
	}

	feed(0, 50)
	if err := m.Drain(); err != nil {
		t.Fatal(err)
	}
	if got := tsPackets(t, slices.Clone(out.Bytes())); got != written {
		t.Errorf("after the first Drain the writer holds %d of the %d packets written", got, written)
	}

	feed(50, 100)
	if err := venc.Flush(video); err != nil {
		t.Fatal(err)
	}
	if err := aenc.Flush(audio); err != nil {
		t.Fatal(err)
	}
	if err := m.Drain(); err != nil {
		t.Fatal(err)
	}
	if got := tsPackets(t, slices.Clone(out.Bytes())); got != written || frames != 100 {
		t.Errorf("after the second Drain the writer holds %d of the %d packets written (%d of 100 frames)", got, written, frames)
	}
}

// memFile is an io.WriteSeeker over memory, as a whole file streamed in
// (offset, data) writes is to cluster-plex's relay.
type memFile struct {
	b   []byte
	off int64
}

func (f *memFile) Write(p []byte) (int, error) {
	if end := f.off + int64(len(p)); end > int64(len(f.b)) {
		f.b = append(f.b, make([]byte, end-int64(len(f.b)))...)
	}
	copy(f.b[f.off:], p)
	f.off += int64(len(p))
	return len(p), nil
}

func (f *memFile) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.off = off
	case io.SeekCurrent:
		f.off += off
	case io.SeekEnd:
		f.off = int64(len(f.b)) + off
	default:
		return 0, errors.New("bad whence")
	}
	return f.off, nil
}

func TestAnMP4MuxedThroughAWriteSeekerPlays(t *testing.T) {
	requireShim(t)
	var file memFile
	m, err := NewMuxerToWriteSeeker(&file, "mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	enc := x264Encoder(t, m.NeedsGlobalHeader(), false)
	vs, err := m.AddEncoderStream(enc, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	write := func(p *Packet) error { return m.WritePacket(vs, p) }
	for i := int64(0); i < 60; i++ {
		f := grayFrame(t, 64, 64, PixelFormatYUV420P)
		f.SetPTS(i)
		if err := enc.Encode(f, write); err != nil {
			t.Fatal(err)
		}
		_ = f.Free()
	}
	if err := enc.Flush(write); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(file.b, []byte("moov")) {
		t.Fatal("no moov box: the muxer did not finish the file")
	}

	d, err := NewDecoderFromReader(bytes.NewReader(file.b), "mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	n := 0
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		n++
	}
	if n != 60 {
		t.Errorf("the muxed file plays %d frames, want 60", n)
	}
}
