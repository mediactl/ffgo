//go:build !ios && !android && (amd64 || arm64)

// cudaprobe loads libcuda.so.1 through purego and calls cuInit and
// cuDeviceGetCount: the first things FFmpeg's CUDA hardware context needs.
// It checks that a container image -- a distroless one under the NVIDIA
// container toolkit, say -- can reach the GPU driver the toolkit injects.
// It prints the device count, or the step that failed and exits non-zero.
package main

import (
	"fmt"
	"os"

	"github.com/ebitengine/purego"
)

func main() {
	lib, err := purego.Dlopen("libcuda.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		fmt.Println("dlopen libcuda.so.1:", err)
		os.Exit(1)
	}
	var cuInit func(uint32) int32
	var cuDeviceGetCount func(*int32) int32
	purego.RegisterLibFunc(&cuInit, lib, "cuInit")
	purego.RegisterLibFunc(&cuDeviceGetCount, lib, "cuDeviceGetCount")
	if rc := cuInit(0); rc != 0 {
		fmt.Println("cuInit:", rc)
		os.Exit(1)
	}
	var n int32
	if rc := cuDeviceGetCount(&n); rc != 0 {
		fmt.Println("cuDeviceGetCount:", rc)
		os.Exit(1)
	}
	fmt.Println("CUDA devices:", n)
}
