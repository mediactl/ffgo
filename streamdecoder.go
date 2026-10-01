//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avcodec"
	"github.com/obinnaokechukwu/ffgo/avformat"
	"github.com/obinnaokechukwu/ffgo/avutil"
)

// StreamDecoderConfig configures a StreamDecoder. A nil config decodes on
// the CPU with FFmpeg's default threading.
type StreamDecoderConfig struct {
	// HWDevice decodes video on the GPU; frames stay in GPU memory.
	HWDevice *HWDevice
	// ExtraHWFrames adds GPU surfaces to the decoder's pool
	// (extra_hw_frames), for an encoder or filters holding frames.
	ExtraHWFrames int
	// Threads is the decoder's thread count; 0 is FFmpeg's default.
	Threads int
}

// StreamDecoder decodes one stream of a Decoder's input from packets the
// caller reads with ReadPacket, so one demuxer can feed a decoder per
// stream, each on its own goroutine (hand them Packet.Clone copies).
type StreamDecoder struct {
	mu       sync.Mutex
	ctx      avcodec.Context
	frame    avutil.Frame
	timeBase Rational
	index    int
	// mayHold: a packet went in and no Receive has answered ErrAgain since,
	// so the decoder may still hold input a flush would drop (FFmpeg 5.1).
	mayHold bool
	closed  bool
}

// NewStreamDecoder opens a decoder for the stream at streamIndex.
func (d *Decoder) NewStreamDecoder(streamIndex int, cfg *StreamDecoderConfig) (*StreamDecoder, error) {
	if cfg == nil {
		cfg = &StreamDecoderConfig{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.formatCtx == nil {
		return nil, ErrClosed
	}
	if streamIndex < 0 || streamIndex >= avformat.GetNumStreams(d.formatCtx) {
		return nil, fmt.Errorf("ffgo: no stream %d", streamIndex)
	}
	stream := avformat.GetStream(d.formatCtx, streamIndex)
	par := avformat.GetStreamCodecPar(stream)
	codec := avcodec.FindDecoder(avformat.GetCodecParCodecID(par))
	if codec == nil {
		return nil, fmt.Errorf("ffgo: no decoder for stream %d", streamIndex)
	}
	ctx := avcodec.AllocContext3(codec)
	if ctx == nil {
		return nil, ErrOutOfMemory
	}
	fail := func(err error) (*StreamDecoder, error) {
		avcodec.FreeContext(&ctx)
		return nil, err
	}
	if err := avcodec.ParametersToContext(ctx, par); err != nil {
		return fail(err)
	}
	num, den := avformat.GetStreamTimeBase(stream)
	if err := avutil.OptSet(unsafe.Pointer(ctx), "pkt_timebase", fmt.Sprintf("%d/%d", num, den), 0); err != nil {
		return fail(fmt.Errorf("ffgo: pkt_timebase: %w", err))
	}
	if cfg.Threads > 0 {
		if err := avutil.OptSetInt(unsafe.Pointer(ctx), "threads", int64(cfg.Threads), 0); err != nil {
			return fail(fmt.Errorf("ffgo: threads: %w", err))
		}
	}
	if cfg.HWDevice != nil {
		avcodec.SetCtxHWDeviceCtx(ctx, cfg.HWDevice.Context())
		if cfg.ExtraHWFrames > 0 {
			if err := avutil.OptSetInt(unsafe.Pointer(ctx), "extra_hw_frames", int64(cfg.ExtraHWFrames), 0); err != nil {
				return fail(fmt.Errorf("ffgo: extra_hw_frames: %w", err))
			}
		}
	}
	if err := avcodec.Open2(ctx, codec, nil); err != nil {
		return fail(err)
	}
	frame := avutil.FrameAlloc()
	if frame == nil {
		return fail(ErrOutOfMemory)
	}
	return &StreamDecoder{ctx: ctx, frame: frame, timeBase: NewRational(num, den), index: streamIndex}, nil
}

// Send gives the decoder a packet of its stream; nil signals end of
// stream, after which Receive drains the decoder until io.EOF. ErrAgain
// means the decoder took nothing: Receive its frames until ErrAgain, then
// send the same packet again. Send(nil) answers ErrAgain until a Receive
// has returned ErrAgain since the last packet: FFmpeg 5.1 drops a packet
// the decoder still buffers when the flush arrives.
func (s *StreamDecoder) Send(p *Packet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	var pkt avcodec.Packet
	if p != nil {
		pkt = p.ptr
	} else if s.mayHold {
		return ErrAgain
	}
	err := avcodec.SendPacketErr(s.ctx, pkt)
	if p != nil && err == nil {
		s.mayHold = true
	}
	return err
}

// Receive returns the next decoded frame: ErrAgain when the decoder needs
// another packet, io.EOF once drained after Send(nil). The frame belongs to
// the decoder and is valid until the next Receive; Clone it to keep it.
func (s *StreamDecoder) Receive() (Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Frame{}, ErrClosed
	}
	avutil.FrameUnref(s.frame)
	if err := avcodec.ReceiveFrame(s.ctx, s.frame); err != nil {
		if errors.Is(err, ErrAgain) {
			s.mayHold = false
		}
		return Frame{}, err
	}
	return Frame{ptr: s.frame, owned: false}, nil
}

// TimeBase is the time base of the frames' timestamps (the stream's).
func (s *StreamDecoder) TimeBase() Rational { return s.timeBase }

// StreamIndex is the stream this decoder decodes.
func (s *StreamDecoder) StreamIndex() int { return s.index }

// HWFramesCtx is the decoder's GPU frame pool, or nil (a CPU decoder, or
// before its first frame). It outlives the frames, so it can be read after
// the decoder is drained.
func (s *StreamDecoder) HWFramesCtx() avutil.HWFramesContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return avcodec.GetCtxHWFramesCtx(s.ctx)
}

// Close frees the decoder; the Decoder it reads from is not affected.
func (s *StreamDecoder) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	avutil.FrameFree(&s.frame)
	avcodec.FreeContext(&s.ctx)
	return nil
}
