//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// NewVideoFrame is an owned black video frame of pf, width x height, with
// its buffers allocated: a source for encoders and trials.
func NewVideoFrame(pf PixelFormat, width, height int) (Frame, error) {
	if err := Init(); err != nil {
		return Frame{}, err
	}
	f := avutil.FrameAlloc()
	if f == nil {
		return Frame{}, ErrOutOfMemory
	}
	avutil.SetFrameWidth(f, int32(width))
	avutil.SetFrameHeight(f, int32(height))
	avutil.SetFrameFormat(f, int32(pf))
	if err := avutil.FrameGetBufferErr(f, 0); err != nil {
		avutil.FrameFree(&f)
		return Frame{}, err
	}
	if ret := shim.FrameFillBlack(unsafe.Pointer(f)); ret < 0 && ret != -38 {
		avutil.FrameFree(&f)
		return Frame{}, avutil.NewError(ret, "av_image_fill_black")
	}
	return Frame{ptr: f, owned: true}, nil
}
