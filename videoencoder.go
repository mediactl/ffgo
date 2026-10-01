//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avcodec"
	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// ErrEncoderCodecMismatch: VideoEncoderConfig names an encoder that does
// not encode its Codec.
var ErrEncoderCodecMismatch = errors.New("ffgo: the named encoder does not encode the configured codec")

// VideoStreamEncoderConfig configures a VideoStreamEncoder.
type VideoStreamEncoderConfig struct {
	VideoEncoderConfig
	// TimeBase is the time base of the frames' PTS (a filter graph's output,
	// or the decoder's), which the packets keep. Required.
	TimeBase Rational
	// SideData is given to the encoder before its first frame
	// (decoded_side_data): mastering display and light level for HDR10.
	SideData map[FrameSideDataType][]byte
	// GlobalHeader sets AV_CODEC_FLAG_GLOBAL_HEADER (Muxer.NeedsGlobalHeader).
	GlobalHeader bool
	// KeepFrameTypes sends frames with their pict_type (a caller forcing
	// keyframes); by default it is cleared so the encoder decides.
	KeepFrameTypes bool
}

// VideoStreamEncoder encodes video frames into packets for a Muxer stream.
// Unlike Encoder it keeps each frame's PTS, and an encoder that answers
// EAGAIN has its packets read and the frame sent again, never dropped.
// CodecOptions the encoder does not have are errors, not ignored.
type VideoStreamEncoder struct {
	mu       sync.Mutex
	ctx      avcodec.Context
	pkt      avcodec.Packet
	timeBase  Rational
	keepTypes bool
	closed    bool
}

// NewVideoStreamEncoder opens a video encoder.
func NewVideoStreamEncoder(cfg VideoStreamEncoderConfig) (*VideoStreamEncoder, error) {
	if err := Init(); err != nil {
		return nil, err
	}
	v := cfg.VideoEncoderConfig
	if v.Width <= 0 || v.Height <= 0 {
		return nil, errors.New("ffgo: width and height must be positive")
	}
	if cfg.TimeBase.Num <= 0 || cfg.TimeBase.Den <= 0 {
		return nil, errors.New("ffgo: a video stream encoder needs the frames' time base")
	}
	var codec avcodec.Codec
	if v.EncoderName != "" {
		if codec = avcodec.FindEncoderByName(v.EncoderName); codec == nil {
			return nil, fmt.Errorf("%w: %s", ErrEncoderNotFound, v.EncoderName)
		}
		if v.Codec != CodecIDNone {
			if id := shim.CodecID(unsafe.Pointer(codec)); id >= 0 && CodecID(id) != avcodec.Resolve(v.Codec) {
				return nil, fmt.Errorf("%w: %s encodes codec %d, not %d", ErrEncoderCodecMismatch, v.EncoderName, id, v.Codec)
			}
		}
	} else {
		if v.Codec == CodecIDNone {
			return nil, errors.New("ffgo: name an encoder or a codec")
		}
		if codec = avcodec.FindEncoder(v.Codec); codec == nil {
			return nil, fmt.Errorf("%w: codec %d", ErrEncoderNotFound, v.Codec)
		}
	}

	pixFmt := v.PixelFormat
	if v.HWFramesCtx != nil {
		f, ok := shim.HWFramesFormat(v.HWFramesCtx)
		if !ok {
			return nil, fmt.Errorf("GPU frames into an encoder: %w", ErrShimRequired)
		}
		pixFmt = PixelFormat(f)
	}
	if pixFmt == PixelFormatNone {
		pixFmt = PixelFormatYUV420P
	}

	ctx := avcodec.AllocContext3(codec)
	if ctx == nil {
		return nil, ErrOutOfMemory
	}
	fail := func(err error) (*VideoStreamEncoder, error) {
		avcodec.FreeContext(&ctx)
		return nil, err
	}
	avcodec.SetCtxWidth(ctx, int32(v.Width))
	avcodec.SetCtxHeight(ctx, int32(v.Height))
	avcodec.SetCtxPixFmt(ctx, int32(pixFmt))
	if v.HWFramesCtx != nil {
		avcodec.SetCtxHWFramesCtx(ctx, v.HWFramesCtx)
	}
	avcodec.SetCtxTimeBase(ctx, cfg.TimeBase.Num, cfg.TimeBase.Den)
	if v.FrameRate.Num > 0 && v.FrameRate.Den > 0 {
		avcodec.SetCtxFramerate(ctx, v.FrameRate.Num, v.FrameRate.Den)
	}
	if v.GOPSize > 0 {
		avcodec.SetCtxGopSize(ctx, int32(v.GOPSize))
	}
	if v.MaxBFrames > 0 {
		avcodec.SetCtxMaxBFrames(ctx, int32(v.MaxBFrames))
	}
	if v.Bitrate > 0 {
		avcodec.SetCtxBitRate(ctx, v.Bitrate)
	}
	typed := v
	typed.CodecOptions = nil
	if err := applyVideoOptions(unsafe.Pointer(ctx), &typed); err != nil {
		return fail(err)
	}
	keys := make([]string, 0, len(v.CodecOptions))
	for k := range v.CodecOptions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := avutil.OptSet(unsafe.Pointer(ctx), k, v.CodecOptions[k], avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			return fail(fmt.Errorf("ffgo: %s option %s=%s: %w", avcodec.GetCodecName(codec), k, v.CodecOptions[k], err))
		}
	}
	for t, data := range cfg.SideData {
		if err := addDecodedSideData(ctx, t, data); err != nil {
			return fail(fmt.Errorf("ffgo: encoder side data: %w", err))
		}
	}
	if cfg.GlobalHeader {
		avcodec.SetCtxFlags(ctx, avcodec.GetCtxFlags(ctx)|avcodec.CodecFlagGlobalHeader)
	}
	if err := avcodec.Open2(ctx, codec, nil); err != nil {
		return fail(fmt.Errorf("ffgo: open %s: %w", avcodec.GetCodecName(codec), err))
	}
	pkt := avcodec.PacketAlloc()
	if pkt == nil {
		return fail(ErrOutOfMemory)
	}
	return &VideoStreamEncoder{ctx: ctx, pkt: pkt, timeBase: cfg.TimeBase, keepTypes: cfg.KeepFrameTypes}, nil
}

// Encode sends f, keeping its PTS, and hands every packet the encoder has
// ready to emit. A packet is valid only during emit.
func (e *VideoStreamEncoder) Encode(f Frame, emit func(*Packet) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if !e.keepTypes {
		// The encoder picks its own frame types; a decoded frame's I/P/B
		// would otherwise force the source's GOP onto the output.
		shim.FrameSetPictTypeNone(unsafe.Pointer(f.ptr))
	}
	return sendDraining(func() error { return avcodec.SendFrameErr(e.ctx, f.ptr) }, func() (int, error) {
		return e.drainCount(emit)
	})
}

// sendDraining sends with send; on EAGAIN it reads the codec's output with
// drain and sends again, so nothing is dropped. A codec that refuses input
// twice while giving no output has broken the send/receive contract and
// is an error rather than a spin.
func sendDraining(send func() error, drain func() (int, error)) error {
	for stalls := 0; ; {
		err := send()
		if errors.Is(err, ErrAgain) {
			n, err := drain()
			if err != nil {
				return err
			}
			if n == 0 {
				if stalls++; stalls == 2 {
					return errors.New("ffgo: the codec neither takes input nor gives output (EAGAIN both ways)")
				}
			}
			continue
		}
		if err != nil {
			return err
		}
		_, err = drain()
		return err
	}
}

// Flush drains the encoder at end of stream.
func (e *VideoStreamEncoder) Flush(emit func(*Packet) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	if err := avcodec.SendFrameErr(e.ctx, nil); err != nil && !avutil.IsEOF(err) {
		return err
	}
	_, err := e.drainCount(emit)
	return err
}

func (e *VideoStreamEncoder) drainCount(emit func(*Packet) error) (int, error) {
	return drainPackets(e.ctx, e.pkt, emit)
}

// drainPackets hands every packet an encoder has ready to emit and says
// how many there were.
func drainPackets(ctx avcodec.Context, pkt avcodec.Packet, emit func(*Packet) error) (int, error) {
	for n := 0; ; n++ {
		avcodec.PacketUnref(pkt)
		err := avcodec.ReceivePacket(ctx, pkt)
		if errors.Is(err, ErrAgain) || avutil.IsEOF(err) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if err := emit(&Packet{ptr: pkt, owned: false}); err != nil {
			return n, err
		}
	}
}

// Parameters are the encoder's codec parameters, for a muxer stream; the
// caller frees them with avcodec.ParametersFree.
func (e *VideoStreamEncoder) Parameters() (avcodec.Parameters, error) {
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

// TimeBase is the time base of the packets' timestamps.
func (e *VideoStreamEncoder) TimeBase() Rational { return e.timeBase }

// Close frees the encoder.
func (e *VideoStreamEncoder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	avcodec.PacketFree(&e.pkt)
	avcodec.FreeContext(&e.ctx)
	return nil
}
