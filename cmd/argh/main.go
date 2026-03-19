package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

// Version is set at build time via ldflags.
var Version = "dev"

// osExit is a variable so tests can intercept os.Exit calls.
var osExit = os.Exit

// checkPlatform returns an error if goos is not "darwin".
// goos is accepted as a parameter so tests can inject values without
// cross-compilation.
func checkPlatform(goos string) error {
	if goos != "darwin" {
		return fmt.Errorf("argh v1 is macOS only")
	}
	return nil
}

func run(out io.Writer, errOut io.Writer, goos string) int {
	if err := checkPlatform(goos); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	fmt.Fprintf(out, "argh %s\n", Version)
	return 0
}

func main() {
	osExit(run(os.Stdout, os.Stderr, runtime.GOOS))
}
