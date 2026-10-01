//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/obinnaokechukwu/ffgo/avutil"
	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// HWFramesConfig describes a GPU frame pool.
type HWFramesConfig struct {
	Device   *HWDevice
	Format   PixelFormat // the hardware format: PixelFormatCUDA(), VAAPI, QSV
	SWFormat PixelFormat // the surfaces' format, e.g. PixelFormatP010LE()
	Width    int
	Height   int
	PoolSize int // surfaces preallocated; 0 lets the pool grow where it can
}

// NewHWFrames makes a GPU frame pool (av_hwframe_ctx_alloc + _init). The
// caller releases it with avutil.FreeBufferRef.
func NewHWFrames(cfg HWFramesConfig) (avutil.HWFramesContext, error) {
	if cfg.Device == nil {
		return nil, errors.New("ffgo: a GPU frame pool needs a device")
	}
	ref, ret := shim.HWFramesNew(unsafe.Pointer(cfg.Device.Context()), int32(cfg.Format), int32(cfg.SWFormat),
		int32(cfg.Width), int32(cfg.Height), int32(cfg.PoolSize))
	if ret == -38 {
		return nil, fmt.Errorf("GPU frame pool: %w", ErrShimRequired)
	}
	if ret < 0 {
		return nil, avutil.NewError(ret, "av_hwframe_ctx_init")
	}
	return avutil.HWFramesContext(ref), nil
}
