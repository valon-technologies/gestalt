// Command sdkgen is the descriptor-driven generator for provider SDKs. A
// plain run regenerates wire stubs and the SDK surfaces and reconciles the
// tree; --check renders the same output to scratch and byte-compares it
// without mutating anything.
//
// Usage:
//
//	sdkgen [--target ts,python,go,rust] [--check]
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/pipeline"
)

func main() {
	var (
		targetsFlag string
		checkFlag   bool
	)
	flag.StringVar(&targetsFlag, "target", "", "comma-separated targets (ts,python,go,rust); default: all")
	flag.StringVar(&targetsFlag, "t", "", "shorthand for --target")
	flag.BoolVar(&checkFlag, "check", false, "render to scratch and report drift without writing")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "sdkgen: unexpected arguments: %v\n", flag.Args())
		flag.Usage()
		os.Exit(2)
	}

	targets, err := emit.ParseTargets(targetsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sdkgen: %v\n", err)
		os.Exit(2)
	}
	repoRoot, err := pipeline.FindRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sdkgen: %v\n", err)
		os.Exit(2)
	}

	err = pipeline.Run(pipeline.Options{
		RepoRoot: repoRoot,
		Targets:  targets,
		Check:    checkFlag,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sdkgen: %v\n", err)
		if errors.Is(err, pipeline.ErrDiagnostics) || errors.Is(err, pipeline.ErrDrift) {
			os.Exit(1)
		}
		os.Exit(2)
	}
}
