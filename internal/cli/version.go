package cli

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// Version is the build version of codex-sync. It is "dev" for local builds and
// is overridden at release time via the linker, e.g.
//
//	go build -ldflags "-X github.com/ahmojo/Codex_Sync/internal/cli.Version=v0.1.10"
var Version = "dev"

// versionString returns the best available version: the linker-injected Version
// when set, otherwise the module version embedded by `go install module@vX`
// (via the build info), otherwise "dev".
func versionString() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

// printVersion writes a one-line version banner including the OS/arch and Go
// version, which is useful in bug reports (Codex's on-disk format can drift, so
// knowing the exact build matters).
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "codex-sync %s (%s/%s, %s)\n", versionString(), runtime.GOOS, runtime.GOARCH, runtime.Version())
}
