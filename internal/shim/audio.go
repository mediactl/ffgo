//go:build !ios && !android && (amd64 || arm64)

package shim

import "unsafe"

var (
	shimChLayoutNbChannels   func(name string) int32
	shimCodecCtxSetChLayout2 func(ctx uintptr, name string) int32
	shimFrameSetChLayout     func(frame uintptr, name string) int32
	shimFrameSetSilence      func(frame uintptr) int32
	shimCodecFirstSampleFmt  func(codec uintptr) int32
	shimAudioFifoAlloc       func(fmt, channels, nb int32) uintptr
	shimAudioFifoFree        func(fifo uintptr)
	shimAudioFifoSize        func(fifo uintptr) int32
	shimAudioFifoWriteFrame  func(fifo, frame uintptr) int32
	shimAudioFifoReadFrame   func(fifo, frame uintptr, nb int32) int32
)

func registerAudioBindings() {
	registerOptionalLibFunc(&shimChLayoutNbChannels, libShim, "ffshim_ch_layout_nb_channels")
	registerOptionalLibFunc(&shimCodecCtxSetChLayout2, libShim, "ffshim_codecctx_set_ch_layout")
	registerOptionalLibFunc(&shimFrameSetChLayout, libShim, "ffshim_frame_set_ch_layout")
	registerOptionalLibFunc(&shimFrameSetSilence, libShim, "ffshim_frame_set_silence")
	registerOptionalLibFunc(&shimCodecFirstSampleFmt, libShim, "ffshim_codec_first_sample_fmt")
	registerOptionalLibFunc(&shimAudioFifoAlloc, libShim, "ffshim_audio_fifo_alloc")
	registerOptionalLibFunc(&shimAudioFifoFree, libShim, "ffshim_audio_fifo_free")
	registerOptionalLibFunc(&shimAudioFifoSize, libShim, "ffshim_audio_fifo_size")
	registerOptionalLibFunc(&shimAudioFifoWriteFrame, libShim, "ffshim_audio_fifo_write_frame")
	registerOptionalLibFunc(&shimAudioFifoReadFrame, libShim, "ffshim_audio_fifo_read_frame")
}

// AudioAvailable reports whether this shim has the audio helpers.
func AudioAvailable() bool { return loaded && shimAudioFifoAlloc != nil }

// ChLayoutNbChannels is the channel count of an FFmpeg layout name, or -1.
func ChLayoutNbChannels(name string) int {
	if !AudioAvailable() {
		return -1
	}
	return int(shimChLayoutNbChannels(name))
}

// CodecCtxSetChLayout sets an AVCodecContext's channel layout by name.
func CodecCtxSetChLayout(ctx unsafe.Pointer, name string) int32 {
	if !AudioAvailable() {
		return averrorENOSYS
	}
	return shimCodecCtxSetChLayout2(uintptr(ctx), name)
}

// FrameSetChLayout sets an AVFrame's channel layout by name.
func FrameSetChLayout(frame unsafe.Pointer, name string) int32 {
	if !AudioAvailable() {
		return averrorENOSYS
	}
	return shimFrameSetChLayout(uintptr(frame), name)
}

// FrameSetSilence fills an audio frame's buffers with silence.
func FrameSetSilence(frame unsafe.Pointer) int32 {
	if !AudioAvailable() {
		return averrorENOSYS
	}
	return shimFrameSetSilence(uintptr(frame))
}

// CodecFirstSampleFmt is an encoder's first supported sample format, or -1.
func CodecFirstSampleFmt(codec unsafe.Pointer) int32 {
	if !AudioAvailable() {
		return -1
	}
	return shimCodecFirstSampleFmt(uintptr(codec))
}

// AudioFifoAlloc allocates an AVAudioFifo; 0 on failure.
func AudioFifoAlloc(fmt, channels, nb int32) uintptr {
	if !AudioAvailable() {
		return 0
	}
	return shimAudioFifoAlloc(fmt, channels, nb)
}

// AudioFifoFree frees an AVAudioFifo.
func AudioFifoFree(fifo uintptr) {
	if AudioAvailable() && fifo != 0 {
		shimAudioFifoFree(fifo)
	}
}

// AudioFifoSize is how many samples per channel the FIFO holds.
func AudioFifoSize(fifo uintptr) int { return int(shimAudioFifoSize(fifo)) }

// AudioFifoWriteFrame appends every sample of an audio frame.
func AudioFifoWriteFrame(fifo uintptr, frame unsafe.Pointer) int32 {
	return shimAudioFifoWriteFrame(fifo, uintptr(frame))
}

// AudioFifoReadFrame reads nb samples into an audio frame's buffers.
func AudioFifoReadFrame(fifo uintptr, frame unsafe.Pointer, nb int) int32 {
	return shimAudioFifoReadFrame(fifo, uintptr(frame), int32(nb))
}
