//go:build !ios && !android && (amd64 || arm64)

package shim

import "unsafe"

var (
	shimEnumValue                func(name string, out *int32) int32
	shimStreamDisposition        func(st uintptr) int32
	shimStreamSetDisposition     func(st uintptr, d int32)
	shimCodecParChLayoutDescribe func(par uintptr, buf *byte, size uintptr) int32
	shimFrameChLayoutDescribe    func(frame uintptr, buf *byte, size uintptr) int32
)

var (
	shimCodecParSAR    func(par uintptr, num, den *int32)
	shimCodecCtxSetSAR func(ctx uintptr, num, den int32)
	shimStreamSetSAR   func(st uintptr, num, den int32)
)

// CodecParSAR is codec parameters' sample aspect ratio; 0/0 without the shim.
func CodecParSAR(par unsafe.Pointer) (num, den int32) {
	if !loaded || shimCodecParSAR == nil || par == nil {
		return 0, 0
	}
	shimCodecParSAR(uintptr(par), &num, &den)
	return num, den
}

// CodecCtxSetSAR sets an encoder's sample aspect ratio; false without the shim.
func CodecCtxSetSAR(ctx unsafe.Pointer, num, den int32) bool {
	if !loaded || shimCodecCtxSetSAR == nil || ctx == nil {
		return false
	}
	shimCodecCtxSetSAR(uintptr(ctx), num, den)
	return true
}

// StreamSetSAR sets a stream's sample aspect ratio (what Matroska writes as
// its display size); false without the shim.
func StreamSetSAR(st unsafe.Pointer, num, den int32) bool {
	if !loaded || shimStreamSetSAR == nil || st == nil {
		return false
	}
	shimStreamSetSAR(uintptr(st), num, den)
	return true
}

func registerStreamBindings() {
	registerOptionalLibFunc(&shimCodecParSAR, libShim, "ffshim_codecpar_sar")
	registerOptionalLibFunc(&shimCodecCtxSetSAR, libShim, "ffshim_codecctx_set_sar")
	registerOptionalLibFunc(&shimStreamSetSAR, libShim, "ffshim_stream_set_sar")
	registerOptionalLibFunc(&shimEnumValue, libShim, "ffshim_enum_value")
	registerOptionalLibFunc(&shimStreamDisposition, libShim, "ffshim_stream_disposition")
	registerOptionalLibFunc(&shimStreamSetDisposition, libShim, "ffshim_stream_set_disposition")
	registerOptionalLibFunc(&shimCodecParChLayoutDescribe, libShim, "ffshim_codecpar_ch_layout_describe")
	registerOptionalLibFunc(&shimFrameChLayoutDescribe, libShim, "ffshim_frame_ch_layout_describe")
}

// EnumValue is a C enum or macro value ("AV_DISPOSITION_FORCED",
// "AV_FRAME_DATA_DOVI_RPU_BUFFER") as the headers the shim was compiled
// against define it; ok is false without a shim that knows the name.
func EnumValue(name string) (int, bool) {
	if !loaded || shimEnumValue == nil {
		return 0, false
	}
	var v int32
	if shimEnumValue(name, &v) != 0 {
		return 0, false
	}
	return int(v), true
}

// StreamDisposition is an AVStream's disposition flags; 0 without the shim.
func StreamDisposition(st unsafe.Pointer) int32 {
	if !loaded || shimStreamDisposition == nil || st == nil {
		return 0
	}
	return shimStreamDisposition(uintptr(st))
}

// SetStreamDisposition sets an AVStream's disposition flags; ok is false
// without the shim.
func SetStreamDisposition(st unsafe.Pointer, d int32) bool {
	if !loaded || shimStreamSetDisposition == nil || st == nil {
		return false
	}
	shimStreamSetDisposition(uintptr(st), d)
	return true
}

// CodecParChLayoutDescribe is an AVCodecParameters' channel layout name
// ("5.1(side)"); "" without the shim or for a stream with no layout.
func CodecParChLayoutDescribe(par unsafe.Pointer) string {
	if !loaded || shimCodecParChLayoutDescribe == nil || par == nil {
		return ""
	}
	return describe(func(b *byte, n uintptr) int32 { return shimCodecParChLayoutDescribe(uintptr(par), b, n) })
}

// FrameChLayoutDescribe is an audio frame's channel layout name; "" without
// the shim.
func FrameChLayoutDescribe(frame unsafe.Pointer) string {
	if !loaded || shimFrameChLayoutDescribe == nil || frame == nil {
		return ""
	}
	return describe(func(b *byte, n uintptr) int32 { return shimFrameChLayoutDescribe(uintptr(frame), b, n) })
}

func describe(f func(*byte, uintptr) int32) string {
	buf := make([]byte, 128)
	if n := f(&buf[0], uintptr(len(buf))); n <= 0 {
		return ""
	}
	for i, c := range buf {
		if c == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
