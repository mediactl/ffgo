//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/obinnaokechukwu/ffgo/internal/shim"
)

// The go/no-go for transcoding in-process on the GPU: NVDEC decodes,
// scale_cuda converts to 10-bit, and hevc_nvenc encodes at the owner's
// archival settings (preset p7, constqp at qp 23, spatial and temporal AQ),
// every frame staying in GPU memory. The ffmpeg CLI used 1.0 s of CPU for
// this clip (40 s of 1080p H.264, 960 frames) on an RTX 2070 Max-Q against
// 14.4 s decoding in software; in-process must be in the same range and lose
// no frame.
func TestNVDECScaleCUDANVENCKeepsFramesOnTheGPU(t *testing.T) {
	if !requireFFmpeg(t) {
		t.Skip("FFmpeg not available")
	}
	dev, err := NewHWDevice(HWDeviceTypeCUDA, "")
	if err != nil {
		t.Skipf("no CUDA device: %v", err)
	}
	defer dev.Close()

	src := filepath.Join(t.TempDir(), "src.mkv")
	makeGPUClip(t, src)

	before := cpuTime(t)
	dec, err := NewHWDecoder(src, &HWDecoderConfig{HWDevice: dev, ExtraHWFrames: 52})
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	first, err := dec.ReadHWFrame()
	if err != nil {
		t.Fatal(err)
	}
	vs := dec.VideoStream()
	graph, err := NewFilterGraph(FilterGraphConfig{
		Width: vs.Width, Height: vs.Height, PixelFmt: PixelFormatCUDA(),
		FrameRate: vs.FrameRate, HWFramesCtx: first.HWFramesCtx(),
		Filters: "scale_cuda=format=p010le",
	})
	if err != nil {
		t.Fatalf("filter graph on GPU frames: %v", err)
	}
	defer graph.Close()

	if f, ok := shim.HWFramesFormat(graph.OutputHWFramesCtx()); !ok || PixelFormat(f) != PixelFormatCUDA() {
		t.Fatalf("scale_cuda's pool format is %d (ok=%v), want CUDA %d", f, ok, PixelFormatCUDA())
	}

	out := filepath.Join(t.TempDir(), "out.mkv")
	var enc *Encoder
	encoded := 0
	push := func(frames []*Frame) {
		for _, f := range frames {
			if enc == nil {
				enc, err = NewEncoderWithOptions(out, &EncoderOptions{Video: &VideoEncoderConfig{
					EncoderName: "hevc_nvenc", Width: vs.Width, Height: vs.Height, FrameRate: vs.FrameRate,
					Profile: ProfileHEVCMain10, HWFramesCtx: graph.OutputHWFramesCtx(),
					CodecOptions: map[string]string{"preset": "p7", "rc": "constqp", "qp": "23", "spatial-aq": "1", "temporal-aq": "1"},
				}})
				if err != nil {
					t.Fatalf("hevc_nvenc on GPU frames: %v", err)
				}
			}
			if err := enc.WriteVideoFrame(*f); err != nil {
				t.Fatalf("frame %d: %v", encoded, err)
			}
			encoded++
			// Each filtered frame holds a GPU surface; the encoder took its
			// own reference.
			_ = FrameFree(f)
		}
	}
	frames, err := graph.Filter(&first)
	if err != nil {
		t.Fatal(err)
	}
	push(frames)
	for {
		f, err := dec.ReadHWFrame()
		if errors.Is(err, io.EOF) || IsEOF(err) {
			// Both checks callers use must see the end of the stream.
			if !errors.Is(err, io.EOF) || !IsEOF(err) {
				t.Fatalf("end of stream %v: errors.Is(io.EOF)=%v, IsEOF=%v", err, errors.Is(err, io.EOF), IsEOF(err))
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		frames, err := graph.Filter(&f)
		if err != nil {
			t.Fatal(err)
		}
		push(frames)
	}
	rest, err := graph.Flush()
	if err != nil {
		t.Fatal(err)
	}
	push(rest)
	if enc == nil {
		t.Fatal("no frame came out of the filter graph")
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	cpu := cpuTime(t) - before

	if encoded != 960 {
		t.Fatalf("encoded %d frames of 960: frames were lost (the decoder must drain at EOF)", encoded)
	}
	got, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,profile,pix_fmt", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "hevc,Main 10,yuv420p10le" {
		t.Fatalf("output %q", got)
	}
	t.Logf("in-process CPU time %v for 960 frames (ffmpeg CLI: 1.0s on NVDEC, 14.4s in software)", cpu)
	if cpu > 4*time.Second {
		t.Fatalf("CPU time %v: frames are not staying on the GPU", cpu)
	}
}

// With no visible GPU, creating the CUDA device is an error, not a hang.
func TestNoCUDADeviceIsAnError(t *testing.T) {
	if os.Getenv("FFGO_CHILD_NO_GPU") == "" {
		if !requireFFmpeg(t) {
			t.Skip("FFmpeg not available")
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestNoCUDADeviceIsAnError$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "FFGO_CHILD_NO_GPU=1", "CUDA_VISIBLE_DEVICES=")
		done := make(chan error, 1)
		var out []byte
		go func() {
			var err error
			out, err = cmd.CombinedOutput()
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatal("creating a CUDA device with no GPU visible hung")
		}
		return
	}
	if _, err := NewHWDevice(HWDeviceTypeCUDA, ""); err == nil {
		t.Fatal("a CUDA device opened with CUDA_VISIBLE_DEVICES empty")
	} else {
		t.Log(err)
	}
}

func cpuTime(t *testing.T) time.Duration {
	t.Helper()
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func makeGPUClip(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=24", "-t", "40",
		"-pix_fmt", "yuv420p", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", path).CombinedOutput()
	if err != nil {
		t.Skipf("cannot make the clip (no ffmpeg CLI): %v %s", err, out)
	}
}

// HWDecoder.DecodeVideo (software frames out) must return every frame too:
// it had ReadHWFrame's bug, returning EOF with the decoder's last frames
// still inside it.
func TestHWDecoderDecodeVideoReturnsEveryFrame(t *testing.T) {
	if !requireFFmpeg(t) {
		t.Skip("FFmpeg not available")
	}
	dev, err := NewHWDevice(HWDeviceTypeCUDA, "")
	if err != nil {
		t.Skipf("no CUDA device: %v", err)
	}
	defer dev.Close()
	src := filepath.Join(t.TempDir(), "src.mkv")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24", "-t", "10",
		"-pix_fmt", "yuv420p", "-c:v", "libx264", "-preset", "veryfast", src).CombinedOutput(); err != nil {
		t.Skipf("cannot make the clip: %v %s", err, out)
	}
	dec, err := NewHWDecoder(src, &HWDecoderConfig{HWDevice: dev, OutputSoftwareFrames: true})
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	n := 0
	for {
		_, err := dec.DecodeVideo()
		if IsEOF(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 240 {
		t.Fatalf("decoded %d frames of 240", n)
	}
}
