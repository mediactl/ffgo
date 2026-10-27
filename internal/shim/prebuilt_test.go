//go:build linux && amd64

package shim

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"

	"github.com/obinnaokechukwu/ffgo/internal/bindings"
)

// The committed prebuilt shim must define every function ffshim.h declares.
// A C change committed without `./shim/build.sh prebuilt` leaves Go calling
// a symbol the shipped library lacks, which the optional bindings would
// silently read as "no shim" (and ShimAPIVersion as 0).
func TestPrebuiltShimDefinesEveryDeclaredFunction(t *testing.T) {
	if err := bindings.Load(); err != nil {
		t.Skipf("FFmpeg not loaded: %v", err)
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	hdr, err := os.ReadFile(filepath.Join(root, "shim", "ffshim.h"))
	if err != nil {
		t.Fatal(err)
	}
	so := filepath.Join(root, "shim", "prebuilt", "linux-amd64", "libffshim.so")
	lib, err := purego.Dlopen(so, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Skipf("the prebuilt shim does not load on this host (built for another FFmpeg?): %v", err)
	}
	defer func() { _ = purego.Dlclose(lib) }()
	decls := regexp.MustCompile(`\b(ffshim_[a-z0-9_]+)\s*\(`).FindAllSubmatch(hdr, -1)
	if len(decls) < 95 {
		t.Fatalf("found %d declarations in ffshim.h, want at least 95 (91 at .11 plus this tag's four)", len(decls))
	}
	for _, m := range decls {
		if _, err := purego.Dlsym(lib, string(m[1])); err != nil {
			t.Errorf("%s is declared in ffshim.h but missing from %s: run ./shim/build.sh prebuilt", m[1], so)
		}
	}
}
