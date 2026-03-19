package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/evanisnor/argh/internal/persistence"
	"github.com/evanisnor/argh/internal/status"
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

// hasArg reports whether flag appears in args.
func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func run(out io.Writer, errOut io.Writer, goos string, args []string) int {
	if err := checkPlatform(goos); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if hasArg(args, "--status") {
		return runStatus(out, errOut, persistence.OSFilesystem{}, time.Now,
			func(fs persistence.Filesystem) (status.Reader, error) {
				return persistence.Open(fs)
			})
	}
	fmt.Fprintf(out, "argh %s\n", Version)
	return 0
}

// runStatus reads PR state from the DB and prints the condensed status line.
// openDB is injected for testability.
func runStatus(
	out io.Writer,
	errOut io.Writer,
	fs persistence.Filesystem,
	now func() time.Time,
	openDB func(persistence.Filesystem) (status.Reader, error),
) int {
	dbPath, err := persistence.DBPath(fs)
	if err != nil {
		fmt.Fprintln(out, "argh: no data")
		return 0
	}
	if _, err := fs.Stat(dbPath); os.IsNotExist(err) {
		fmt.Fprintln(out, "argh: no data")
		return 0
	} else if err != nil {
		fmt.Fprintf(errOut, "argh: %v\n", err)
		return 1
	}

	r, err := openDB(fs)
	if err != nil {
		fmt.Fprintf(errOut, "argh: cannot open db: %v\n", err)
		return 1
	}
	defer r.Close()

	line, err := status.Compute(r, now)
	if err != nil {
		fmt.Fprintf(errOut, "argh: %v\n", err)
		return 1
	}
	fmt.Fprintln(out, line.String())
	return 0
}

func main() {
	osExit(run(os.Stdout, os.Stderr, runtime.GOOS, os.Args[1:]))
}
