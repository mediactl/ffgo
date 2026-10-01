//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"

	"github.com/obinnaokechukwu/ffgo/avcodec"
	"github.com/obinnaokechukwu/ffgo/avutil"
)

// hdr10Clip makes a 10-bit HEVC clip whose SEI carries mastering display
// and light level (libx265's x265-params), in Matroska.
func hdr10Clip(t *testing.T) string {
	t.Helper()
	ffmpegOrSkip(t)
	path := filepath.Join(t.TempDir(), "hdr10.mkv")
	run(t, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=256x144:rate=24:duration=1",
		"-pix_fmt", "yuv420p10le", "-c:v", "libx265",
		"-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc",
		"-x265-params", "log-level=error:hdr10=1:repeat-headers=1:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc:"+
			"master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400",
		path)
	return path
}

// softwareFrame is an owned 64x64 yuv420p frame with buffers.
func softwareFrame(t *testing.T) Frame {
	t.Helper()
	f := FrameAlloc()
	if f.IsNil() {
		t.Fatal("frame alloc")
	}
	avutil.SetFrameWidth(f.ptr, 64)
	avutil.SetFrameHeight(f.ptr, 64)
	avutil.SetFrameFormat(f.ptr, int32(PixelFormatYUV420P))
	if err := avutil.FrameGetBufferErr(f.ptr, 0); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDecodedFramesCarryMasteringDisplayAndLightLevel(t *testing.T) {
	requireShim(t)
	d, err := NewDecoder(hdr10Clip(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	f, err := d.DecodeVideo()
	if err != nil {
		t.Fatal(err)
	}
	if md, ok := f.SideData(FrameSideMasteringDisplay()); !ok || len(md) == 0 {
		t.Errorf("mastering display side data: ok=%v len=%d", ok, len(md))
	}
	cll, ok := f.SideData(FrameSideContentLightLevel())
	if !ok || len(cll) != 8 {
		t.Fatalf("light level side data: ok=%v len=%d, want 8 bytes (MaxCLL, MaxFALL)", ok, len(cll))
	}
	if maxCLL, maxFALL := binary.LittleEndian.Uint32(cll[0:4]), binary.LittleEndian.Uint32(cll[4:8]); maxCLL != 1000 || maxFALL != 400 {
		t.Errorf("MaxCLL, MaxFALL = %d, %d; want 1000, 400", maxCLL, maxFALL)
	}
}

func TestRemoveSideDataDropsOnlyTheNamedTypes(t *testing.T) {
	requireShim(t)
	f := softwareFrame(t)
	defer f.Free()
	if err := f.AddSideData(FrameSideHDRPlus(), []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := f.AddSideData(FrameSideContentLightLevel(), make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.SideData(FrameSideHDRPlus()); !ok || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("added HDR10+ side data reads back as %v, %v", got, ok)
	}
	f.RemoveSideData(FrameSideHDRPlus(), FrameSideDOVIRPU(), FrameSideDOVIMetadata())
	if _, ok := f.SideData(FrameSideHDRPlus()); ok {
		t.Error("HDR10+ side data still on the frame")
	}
	if _, ok := f.SideData(FrameSideContentLightLevel()); !ok {
		t.Error("light level side data was removed too")
	}
}

func TestStreamSideDataRoundTrip(t *testing.T) {
	requireShim(t)
	par := avcodec.ParametersAlloc()
	if par == nil {
		t.Fatal("avcodec_parameters_alloc")
	}
	defer avcodec.ParametersFree(&par)
	want := []byte{0xe8, 3, 0, 0, 0x90, 1, 0, 0}
	err := SetStreamSideData(par, PacketSideContentLightLevel(), want)
	if errors.Is(err, ErrNotSupported) {
		t.Skip("stream coded side data needs FFmpeg 6.1")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := SetStreamSideData(par, PacketSideContentLightLevel(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := StreamSideData(par, PacketSideContentLightLevel())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !bytes.Equal(got, want) {
		t.Errorf("got %v, %v; want %v", got, ok, want)
	}
	if n := streamSideDataCount(par); n != 1 {
		t.Errorf("%d side data entries after setting one type twice, want 1", n)
	}
}
