package main

import (
	"fmt"
	"io"
	"os"
)

// Version is set at build time via ldflags.
var Version = "dev"

func run(out io.Writer) {
	fmt.Fprintf(out, "argh %s\n", Version)
}

func main() {
	run(os.Stdout)
}
