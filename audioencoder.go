//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avcodec"
	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// AudioEncoderConfig2 configures an AudioEncoder, the packet-level audio
// encoder (upstream's AudioEncoderConfig belongs to Encoder).
type AudioEncoderConfig2 struct {
	// EncoderName is the encoder ("aac" when empty).
	EncoderName string
	// SampleRate is the output sample rate; input frames must match it.
	SampleRate int
	// Layout is the output channel layout name ("5.1", "stereo"); input
	// frames must be in it.
	Layout string
	// BitRate is the target bit rate in bits/second.
	BitRate int64
	// GlobalHeader sets AV_CODEC_FLAG_GLOBAL_HEADER (Muxer.NeedsGlobalHeader).
	GlobalHeader bool
	// InputTimeBase is the time base of input frames' PTS; zero means
	// 1/SampleRate. Only the first frame's PTS is read: output timestamps
	// start there and advance by samples, so there are no gaps or overlaps.
	InputTimeBase Rational
}

// AudioEncoder encodes audio frames of any size into packets. An audio
// FIFO cuts the input into the encoder's frame size (1024 samples for AAC)
// and the last, short frame goes out at Flush.
type AudioEncoder struct {
	mu         sync.Mutex
	ctx        avcodec.Context
	pkt        avcodec.Packet
	fifo       uintptr
	frameSize  int
	sampleRate int
	sampleFmt  SampleFormat
	channels   int
	layout     string
	inTB       Rational
	nextPTS    int64
	havePTS    bool
	closed     bool
}

// NewAudioEncoder opens an audio encoder.
func NewAudioEncoder(cfg AudioEncoderConfig2) (*AudioEncoder, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	if !shim.AudioAvailable() {
		return nil, fmt.Errorf("audio encoder: %w", ErrShimRequired)
	}
	if cfg.EncoderName == "" {
		cfg.EncoderName = "aac"
	}
	if cfg.SampleRate <= 0 || cfg.Layout == "" || cfg.BitRate <= 0 {
		return nil, errors.New("ffgo: audio encoder needs a sample rate, a layout and a bit rate")
	}
	channels := shim.ChLayoutNbChannels(cfg.Layout)
	if channels <= 0 {
		return nil, fmt.Errorf("ffgo: unknown channel layout %q", cfg.Layout)
	}
	codec := avcodec.FindEncoderByName(cfg.EncoderName)
	if codec == nil {
		return nil, fmt.Errorf("%w: %s", ErrEncoderNotFound, cfg.EncoderName)
	}
	sampleFmt := SampleFormat(shim.CodecFirstSampleFmt(unsafe.Pointer(codec)))
	if sampleFmt < 0 {
		sampleFmt = SampleFormatFLTP
	}
	ctx := avcodec.AllocContext3(codec)
	if ctx == nil {
		return nil, ErrOutOfMemory
	}
	fail := func(err error) (*AudioEncoder, error) {
		avcodec.FreeContext(&ctx)
		return nil, err
	}
	avcodec.SetCtxSampleRate(ctx, int32(cfg.SampleRate))
	avcodec.SetCtxSampleFmt(ctx, int32(sampleFmt))
	if ret := shim.CodecCtxSetChLayout(unsafe.Pointer(ctx), cfg.Layout); ret < 0 {
		return fail(avutil.NewError(ret, "av_channel_layout_from_string "+cfg.Layout))
	}
	avcodec.SetCtxBitRate(ctx, cfg.BitRate)
	avcodec.SetCtxTimeBase(ctx, 1, int32(cfg.SampleRate))
	if cfg.GlobalHeader {
		avcodec.SetCtxFlags(ctx, avcodec.GetCtxFlags(ctx)|avcodec.CodecFlagGlobalHeader)
	}
	if err := avcodec.Open2(ctx, codec, nil); err != nil {
		return fail(fmt.Errorf("ffgo: open %s: %w", cfg.EncoderName, err))
	}
	frameSize := avcodec.GetCtxFrameSize(ctx)
	if frameSize <= 0 {
		frameSize = 1024 // an encoder taking any size (PCM): chunk anyway
	}
	fifo := shim.AudioFifoAlloc(int32(sampleFmt), int32(channels), int32(frameSize*2))
	if fifo == 0 {
		return fail(ErrOutOfMemory)
	}
	pkt := avcodec.PacketAlloc()
	if pkt == nil {
		shim.AudioFifoFree(fifo)
		return fail(ErrOutOfMemory)
	}
	inTB := cfg.InputTimeBase
	if inTB.Num == 0 || inTB.Den == 0 {
		inTB = NewRational(1, int32(cfg.SampleRate))
	}
	return &AudioEncoder{
		ctx: ctx, pkt: pkt, fifo: fifo, frameSize: frameSize, sampleRate: cfg.SampleRate,
		sampleFmt: sampleFmt, channels: channels, layout: cfg.Layout, inTB: inTB,
	}, nil
}

// Encode queues f's samples and encodes every whole frame they complete,
// handing each packet to emit. A packet is valid only during emit (the
// Muxer's WritePacket takes what it needs).
func (e *AudioEncoder) Encode(f Frame, emit func(*Packet) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if f.IsNil() {
		return nil
	}
	// The FIFO reads as many planes as the encoder has channels: a frame of
	// another format, layout or rate would be read out of bounds or played
	// at the wrong speed, so it is refused.
	if got := SampleFormat(f.Format()); got != e.sampleFmt {
		return fmt.Errorf("ffgo: audio encoder takes sample format %d, the frame is %d", e.sampleFmt, got)
	}
	if got := shim.FrameNbChannels(unsafe.Pointer(f.ptr)); got != e.channels {
		return fmt.Errorf("ffgo: audio encoder takes %d channels (%s), the frame has %d", e.channels, e.layout, got)
	}
	if got := int(avutil.GetFrameSampleRate(f.ptr)); got != e.sampleRate {
		return fmt.Errorf("ffgo: audio encoder takes %d Hz, the frame is %d Hz", e.sampleRate, got)
	}
	toSamples := NewRational(1, int32(e.sampleRate))
	if pts := f.PTS(); pts != avutil.AV_NOPTS_VALUE {
		at := rescaleQ(pts, e.inTB, toSamples)
		if !e.havePTS {
			e.nextPTS = at
		} else if gap := at - (e.nextPTS + int64(shim.AudioFifoSize(e.fifo))); gap > int64(e.frameSize) {
			// Samples missing from the input (dropped packets): fill them
			// with silence so later audio keeps its timestamps. A gap of
			// more than a minute is a broken timestamp, not a gap.
			if gap <= int64(60*e.sampleRate) {
				if err := e.writeSilence(int(gap)); err != nil {
					return err
				}
			}
		}
	}
	e.havePTS = true
	if ret := shim.AudioFifoWriteFrame(e.fifo, unsafe.Pointer(f.ptr)); ret < 0 {
		return avutil.NewError(ret, "av_audio_fifo_write")
	}
	for shim.AudioFifoSize(e.fifo) >= e.frameSize {
		if err := e.sendFromFifo(e.frameSize, emit); err != nil {
			return err
		}
	}
	return nil
}

// writeSilence appends n silent samples to the FIFO.
func (e *AudioEncoder) writeSilence(n int) error {
	for n > 0 {
		chunk := min(n, 48000)
		s, err := NewAudioFrame(e.sampleFmt, e.sampleRate, e.layout, chunk)
		if err != nil {
			return err
		}
		ret := shim.AudioFifoWriteFrame(e.fifo, unsafe.Pointer(s.ptr))
		_ = s.Free()
		if ret < 0 {
			return avutil.NewError(ret, "av_audio_fifo_write")
		}
		n -= chunk
	}
	return nil
}

// Flush encodes the samples left in the FIFO as a short last frame, then
// drains the encoder.
func (e *AudioEncoder) Flush(emit func(*Packet) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if n := shim.AudioFifoSize(e.fifo); n > 0 {
		if err := e.sendFromFifo(n, emit); err != nil {
			return err
		}
	}
	if err := avcodec.SendFrameErr(e.ctx, nil); err != nil && !avutil.IsEOF(err) {
		return err
	}
	_, err := drainPackets(e.ctx, e.pkt, emit)
	return err
}

// sendFromFifo sends n samples from the FIFO as one frame. A fresh frame
// each time: the encoder may keep a reference to the one it was sent.
func (e *AudioEncoder) sendFromFifo(n int, emit func(*Packet) error) error {
	f, err := NewAudioFrame(e.sampleFmt, e.sampleRate, e.layout, n)
	if err != nil {
		return err
	}
	defer f.Free()
	if ret := shim.AudioFifoReadFrame(e.fifo, unsafe.Pointer(f.ptr), n); int(ret) < n {
		return avutil.NewError(ret, "av_audio_fifo_read")
	}
	f.SetPTS(e.nextPTS)
	e.nextPTS += int64(n)
	return sendDraining(func() error { return avcodec.SendFrameErr(e.ctx, f.ptr) }, func() (int, error) {
		return drainPackets(e.ctx, e.pkt, emit)
	})
}

// Parameters are the encoder's codec parameters, for a muxer stream; the
// caller frees them with avcodec.ParametersFree.
func (e *AudioEncoder) Parameters() (avcodec.Parameters, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	par := avcodec.ParametersAlloc()
	if par == nil {
		return nil, ErrOutOfMemory
	}
	if err := avcodec.ParametersFromContext(par, e.ctx); err != nil {
		avcodec.ParametersFree(&par)
		return nil, err
	}
	return par, nil
}

// TimeBase is the time base of the packets' timestamps (1/sample rate).
func (e *AudioEncoder) TimeBase() Rational { return NewRational(1, int32(e.sampleRate)) }

// FrameSize is the encoder's samples per frame.
func (e *AudioEncoder) FrameSize() int { return e.frameSize }

// Close frees the encoder.
func (e *AudioEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	shim.AudioFifoFree(e.fifo)
	avcodec.PacketFree(&e.pkt)
	avcodec.FreeContext(&e.ctx)
	return nil
}

// NewAudioFrame is an owned audio frame of n silent samples in format fmt,
// at rate, in the named channel layout.
func NewAudioFrame(fmt_ SampleFormat, rate int, layout string, n int) (Frame, error) {
	if !shim.AudioAvailable() {
		return Frame{}, fmt.Errorf("audio frame: %w", ErrShimRequired)
	}
	f := avutil.FrameAlloc()
	if f == nil {
		return Frame{}, ErrOutOfMemory
	}
	fail := func(err error) (Frame, error) {
		avutil.FrameFree(&f)
		return Frame{}, err
	}
	avutil.FrameSetSampleRate(f, int32(rate))
	avutil.FrameSetFormat(f, int32(fmt_))
	avutil.FrameSetNbSamples(f, int32(n))
	if ret := shim.FrameSetChLayout(unsafe.Pointer(f), layout); ret < 0 {
		return fail(avutil.NewError(ret, "av_channel_layout_from_string "+layout))
	}
	if err := avutil.FrameGetBufferErr(f, 0); err != nil {
		return fail(err)
	}
	if ret := shim.FrameSetSilence(unsafe.Pointer(f)); ret < 0 {
		return fail(avutil.NewError(ret, "av_samples_set_silence"))
	}
	return Frame{ptr: f, owned: true}, nil
}

// rescaleQ is av_rescale_q (round to nearest, halves away from zero) in Go:
// a * from / to without overflow.
func rescaleQ(a int64, from, to Rational) int64 {
	num := new(big.Int).Mul(big.NewInt(a), big.NewInt(int64(from.Num)*int64(to.Den)))
	den := big.NewInt(int64(from.Den) * int64(to.Num))
	if den.Sign() == 0 {
		return a
	}
	if den.Sign() < 0 {
		num.Neg(num)
		den.Neg(den)
	}
	half := new(big.Int).Rsh(den, 1)
	if num.Sign() >= 0 {
		num.Add(num, half)
	} else {
		num.Sub(num, half)
	}
	return new(big.Int).Quo(num, den).Int64()
}
