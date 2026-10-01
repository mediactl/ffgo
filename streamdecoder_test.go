//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOneDemuxerDecodesEveryStreamOnItsOwnGoroutine(t *testing.T) {
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "multi.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=3,aformat=channel_layouts=5.1",
		"-map", "0", "-map", "1", "-map", "2", "-c:v", "libx264", "-c:a:0", "aac", "-c:a:1", "flac", path)
	d, err := NewDecoder(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	streams := d.Streams()
	chans := map[int]chan *Packet{}
	counts := make([]atomic.Int64, len(streams))
	errs := make(chan error, len(streams))
	var wg sync.WaitGroup
	for _, s := range streams {
		sd, err := d.NewStreamDecoder(s.Index, nil)
		if err != nil {
			t.Fatal(err)
		}
		ch := make(chan *Packet, 4)
		chans[s.Index] = ch
		wg.Add(1)
		go func(idx int, sd *StreamDecoder, ch chan *Packet) {
			defer wg.Done()
			defer sd.Close()
			drain := func() error {
				for {
					_, err := sd.Receive()
					if errors.Is(err, ErrAgain) || errors.Is(err, io.EOF) {
						return nil
					}
					if err != nil {
						return err
					}
					counts[idx].Add(1)
				}
			}
			for p := range ch {
				err := sd.Send(p)
				_ = p.Free()
				if err == nil {
					err = drain()
				}
				if err != nil {
					errs <- err
					for range ch {
					}
					return
				}
			}
			if err := sd.Send(nil); err != nil {
				errs <- err
				return
			}
			if err := drain(); err != nil {
				errs <- err
			}
		}(s.Index, sd, ch)
	}
	for {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			break
		}
		c, err := p.Clone()
		if err != nil {
			t.Fatal(err)
		}
		chans[p.StreamIndex()] <- c
	}
	for _, ch := range chans {
		close(ch)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for i := range counts {
		if got, want := counts[i].Load(), ffprobeFrameCount(t, path, i); got != want {
			t.Errorf("stream %d: decoded %d frames, ffprobe counts %d", i, got, want)
		}
	}
}

func TestStreamDecoderKeepsNVDECFramesOnTheGPU(t *testing.T) {
	dev := cudaOrSkip(t)
	d, err := NewDecoder(h264Clip(t, 48))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	vi := d.VideoStream().Index
	sd, err := d.NewStreamDecoder(vi, &StreamDecoderConfig{HWDevice: dev, ExtraHWFrames: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer sd.Close()
	n := 0
	for done := false; !done; {
		p, err := d.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case p == nil:
			err, done = sd.Send(nil), true
		case p.StreamIndex() == vi:
			err = sd.Send(p)
		default:
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for {
			f, err := sd.Receive()
			if errors.Is(err, ErrAgain) || errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := PixelFormat(f.Format()); got != PixelFormatCUDA() {
				t.Fatalf("frame %d in pixel format %d, want CUDA (%d): frames left the GPU", n, got, PixelFormatCUDA())
			}
			n++
		}
	}
	if n != 48 {
		t.Errorf("decoded %d frames, want 48", n)
	}
	if sd.HWFramesCtx() == nil {
		t.Error("no GPU frame pool reported")
	}
}
