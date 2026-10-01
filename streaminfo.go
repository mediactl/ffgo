//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avformat"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// Disposition is a stream's AV_DISPOSITION_* flags.
type Disposition int32

// FFmpeg's AV_DISPOSITION_* values (libavformat/avformat.h; stable across
// releases, and checked against the shim's headers by a test).
const (
	DispositionDefault         Disposition = 0x1
	DispositionDub             Disposition = 0x2
	DispositionOriginal        Disposition = 0x4
	DispositionComment         Disposition = 0x8
	DispositionLyrics          Disposition = 0x10
	DispositionKaraoke         Disposition = 0x20
	DispositionForced          Disposition = 0x40
	DispositionHearingImpaired Disposition = 0x80
	DispositionVisualImpaired  Disposition = 0x100
	DispositionCleanEffects    Disposition = 0x200
	DispositionAttachedPic     Disposition = 0x400
	DispositionCaptions        Disposition = 0x10000
	DispositionDescriptions    Disposition = 0x20000
	DispositionMetadata        Disposition = 0x40000
)

// Streams describes every stream of the input, in index order, with its
// tags, disposition and (audio) channel layout name. Disposition and
// ChannelLayout need the shim and are zero without it.
func (d *Decoder) Streams() []*StreamInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.formatCtx == nil {
		return nil
	}
	n := avformat.GetNumStreams(d.formatCtx)
	out := make([]*StreamInfo, 0, n)
	for i := 0; i < n; i++ {
		info := d.getStreamInfo(i)
		if info == nil {
			continue
		}
		stream := avformat.GetStream(d.formatCtx, i)
		info.Disposition = Disposition(shim.StreamDisposition(unsafe.Pointer(stream)))
		info.Metadata = getMetadataFromDict(avformat.GetStreamMetadata(stream))
		info.BitRate = avformat.GetCodecParBitRate(unsafe.Pointer(info.codecPar))
		info.Language = info.Metadata["language"]
		info.Title = info.Metadata["title"]
		if info.Type == MediaTypeVideo {
			if n, d := shim.CodecParSAR(unsafe.Pointer(info.codecPar)); n > 0 && d > 0 {
				info.SampleAspectRatio = NewRational(n, d)
			}
		}
		if info.Type == MediaTypeAudio {
			info.ChannelLayout = shim.CodecParChLayoutDescribe(unsafe.Pointer(info.codecPar))
		}
		out = append(out, info)
	}
	return out
}
