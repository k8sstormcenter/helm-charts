package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("rulelint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	write := flags.Bool("write", false, "add or update the portability tag of every rule in the file")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: rulelint [--write] <rules file>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 2
	}
	path := flags.Arg(0)
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	results, updated, err := lint(string(src), *write)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return 2
	}
	if *write && updated != string(src) {
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	failed := false
	for _, r := range results {
		reasons := "-"
		if len(r.Reasons) > 0 {
			reasons = strings.Join(r.Reasons, "; ")
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", r.ID, r.Class, r.Name, reasons)
		for _, problem := range r.Problems {
			failed = true
			fmt.Fprintf(stderr, "%s: rule %s: %s\n", path, r.ID, problem)
		}
	}
	if failed {
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
