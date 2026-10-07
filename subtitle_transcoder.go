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
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

var (
	// ErrSubtitleDecode wraps a packet the subtitle decoder refused; ffmpeg(1)
	// logs it and goes on (ffmpeg_dec.c), so a caller can skip the packet.
	ErrSubtitleDecode = errors.New("ffgo: subtitle decode failed")
	// ErrSubtitleEncode wraps an encoder failure, which ffmpeg(1) treats as fatal.
	ErrSubtitleEncode = errors.New("ffgo: subtitle encode failed")
)

// SubtitleEvent is one encoded subtitle: PTS and Duration in microseconds
// (AV_TIME_BASE), Data the encoder's output.
type SubtitleEvent struct {
	PTS      int64
	Duration int64
	Data     []byte
}

const (
	subtitleOutMax     = 1 << 20 // fftools/ffmpeg_enc.c subtitle_out_max_size
	subtitleStructSize = 32      // sizeof(AVSubtitle), FFmpeg 4 through 9
)

// SubtitleTranscoder decodes one subtitle stream and re-encodes each event
// with another subtitle encoder, as `ffmpeg -map 0:N -c:s <encoder>` does.
type SubtitleTranscoder struct {
	mu     sync.Mutex
	dec    avcodec.Context
	enc    avcodec.Context
	sub    unsafe.Pointer // AVSubtitle
	out    unsafe.Pointer // subtitleOutMax bytes
	flush  avcodec.Packet // an empty packet: the decoder flush
	index  int
	closed bool
}

// NewSubtitleTranscoder opens a decoder for subtitle stream index (with
// pkt_timebase, so event times are filled) and the named encoder ("srt") in
// AV_TIME_BASE, given the decoder's subtitle_header as ffmpeg(1) gives it.
func (d *Decoder) NewSubtitleTranscoder(index int, encoder string) (*SubtitleTranscoder, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.formatCtx == nil {
		return nil, ErrClosed
	}
	if index < 0 || index >= avformat.GetNumStreams(d.formatCtx) {
		return nil, fmt.Errorf("ffgo: no stream %d", index)
	}
	stream := avformat.GetStream(d.formatCtx, index)
	par := avformat.GetStreamCodecPar(stream)
	if avformat.GetCodecParType(par) != avutil.MediaTypeSubtitle {
		return nil, fmt.Errorf("ffgo: stream %d is not a subtitle stream", index)
	}
	t := &SubtitleTranscoder{index: index}
	fail := func(err error) (*SubtitleTranscoder, error) { t.free(); return nil, err }
	dc := avcodec.FindDecoder(avformat.GetCodecParCodecID(par))
	if dc == nil {
		return fail(fmt.Errorf("ffgo: no decoder for stream %d", index))
	}
	if t.dec = avcodec.AllocContext3(dc); t.dec == nil {
		return fail(ErrOutOfMemory)
	}
	if err := avcodec.ParametersToContext(t.dec, par); err != nil {
		return fail(err)
	}
	num, den := avformat.GetStreamTimeBase(stream)
	if err := avutil.OptSet(unsafe.Pointer(t.dec), "pkt_timebase", fmt.Sprintf("%d/%d", num, den), 0); err != nil {
		return fail(fmt.Errorf("ffgo: pkt_timebase: %w", err))
	}
	if err := avcodec.Open2(t.dec, dc, nil); err != nil {
		return fail(err)
	}
	ec := avcodec.FindEncoderByName(encoder)
	if ec == nil {
		return fail(fmt.Errorf("ffgo: no subtitle encoder %q", encoder))
	}
	if t.enc = avcodec.AllocContext3(ec); t.enc == nil {
		return fail(ErrOutOfMemory)
	}
	avcodec.SetCtxTimeBase(t.enc, 1, 1000000)
	if ret := shim.CodecCtxCopySubtitleHeader(unsafe.Pointer(t.enc), unsafe.Pointer(t.dec)); ret < 0 {
		if ret == -38 {
			return fail(fmt.Errorf("ffgo: subtitle header copy: %w", ErrShimRequired))
		}
		return fail(avutil.NewError(ret, "subtitle_header"))
	}
	if err := avcodec.Open2(t.enc, ec, nil); err != nil {
		return fail(err)
	}
	if t.sub = avutil.Malloc(subtitleStructSize); t.sub == nil {
		return fail(ErrOutOfMemory)
	}
	clearSubtitle(t.sub)
	if t.out = avutil.Malloc(subtitleOutMax); t.out == nil {
		return fail(ErrOutOfMemory)
	}
	if t.flush = avcodec.PacketAlloc(); t.flush == nil {
		return fail(ErrOutOfMemory)
	}
	return t, nil
}

// Transcode decodes p (a packet of the transcoder's stream; nil flushes the
// decoder at end of file) and encodes the event. ok is false when there is
// no output: no event, an event without pts, or an empty encode. The event's
// PTS and Duration are the decoded ones; the encoder sees start_display_time
// folded into pts, as fftools/ffmpeg_enc.c do_subtitle_out folds it.
func (t *SubtitleTranscoder) Transcode(p *Packet) (SubtitleEvent, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return SubtitleEvent{}, false, ErrClosed
	}
	pkt := t.flush
	if p != nil {
		if p.StreamIndex() != t.index {
			return SubtitleEvent{}, false, nil
		}
		pkt = p.ptr
	}
	clearSubtitle(t.sub)
	got, err := avcodec.DecodeSubtitle2(t.dec, t.sub, unsafe.Pointer(pkt))
	if err != nil {
		return SubtitleEvent{}, false, fmt.Errorf("%w: %w", ErrSubtitleDecode, err)
	}
	if !got {
		return SubtitleEvent{}, false, nil
	}
	defer avcodec.SubtitleFree(t.sub)
	pts := *(*int64)(unsafe.Add(t.sub, offsetSubPTS))
	if pts == avutil.NoPTSValue {
		return SubtitleEvent{}, false, nil
	}
	start := *(*uint32)(unsafe.Add(t.sub, offsetSubStartDisplayTime))
	end := *(*uint32)(unsafe.Add(t.sub, offsetSubEndDisplayTime))
	*(*int64)(unsafe.Add(t.sub, offsetSubPTS)) = pts + int64(start)*1000
	*(*uint32)(unsafe.Add(t.sub, offsetSubEndDisplayTime)) = end - start
	*(*uint32)(unsafe.Add(t.sub, offsetSubStartDisplayTime)) = 0
	n := avcodec.EncodeSubtitle(t.enc, t.out, subtitleOutMax, t.sub)
	if n < 0 {
		return SubtitleEvent{}, false, fmt.Errorf("%w: %w", ErrSubtitleEncode, avutil.NewError(n, "avcodec_encode_subtitle"))
	}
	if n == 0 {
		return SubtitleEvent{}, false, nil
	}
	data := make([]byte, n)
	copy(data, unsafe.Slice((*byte)(t.out), n))
	return SubtitleEvent{PTS: pts, Duration: int64(end) * 1000, Data: data}, true, nil
}

// Close frees the transcoder; the Decoder it reads from is not affected.
func (t *SubtitleTranscoder) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		t.free()
	}
	return nil
}

func (t *SubtitleTranscoder) free() {
	if t.flush != nil {
		avcodec.PacketFree(&t.flush)
	}
	if t.out != nil {
		avutil.Free(t.out)
		t.out = nil
	}
	if t.sub != nil {
		avutil.Free(t.sub)
		t.sub = nil
	}
	if t.enc != nil {
		avcodec.FreeContext(&t.enc)
	}
	if t.dec != nil {
		avcodec.FreeContext(&t.dec)
	}
}
