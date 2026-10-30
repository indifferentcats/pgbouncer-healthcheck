// Command testreport turns `go test -json` output into readable logs and a
// Markdown summary for GitHub Actions, without third-party tools.
//
//	go test -json ./... | testreport -stream          # echo test output
//	testreport -summary results.json >> "$GITHUB_STEP_SUMMARY"
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
)

// event is a subset of cmd/test2json's TestEvent.
type event struct {
	Action  string
	Package string
	Test    string
	Output  string
}

func main() {
	stream := flag.Bool("stream", false, "echo test output from stdin")
	summary := flag.String("summary", "", "write a Markdown summary of this results file")
	flag.Parse()

	switch {
	case *stream:
		if err := echo(os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
	case *summary != "":
		f, err := os.Open(*summary)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := writeSummary(f, os.Stdout); err != nil {
			log.Fatal(err)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func events(r io.Reader, fn func(event)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue // not a JSON event (e.g. stray output)
		}
		fn(e)
	}
	return sc.Err()
}

// echo prints test and build output, including compile errors, which
// go test -json reports as "build-output".
func echo(r io.Reader, w io.Writer) error {
	return events(r, func(e event) {
		if e.Action == "output" || e.Action == "build-output" {
			fmt.Fprint(w, e.Output)
		}
	})
}

func writeSummary(r io.Reader, w io.Writer) error {
	var passed, failed, skipped, pkgFailed int
	var failures []string
	err := events(r, func(e event) {
		switch {
		case e.Test != "" && e.Action == "pass":
			passed++
		case e.Test != "" && e.Action == "skip":
			skipped++
		case e.Test != "" && e.Action == "fail":
			failed++
			failures = append(failures, e.Package+" "+e.Test)
		case e.Test == "" && e.Action == "fail":
			// Package-level failure: build error, panic, timeout, or failing tests
			pkgFailed++
			failures = append(failures, e.Package+" (package)")
		}
	})
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "## Unit tests")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| ✅ Passed | ❌ Failed | ⏭️ Skipped | 📦 Packages failed |")
	fmt.Fprintln(w, "|---|---|---|---|")
	fmt.Fprintf(w, "| %d | %d | %d | %d |\n", passed, failed, skipped, pkgFailed)
	if len(failures) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "### Failures")
		for _, f := range failures {
			fmt.Fprintf(w, "- `%s`\n", f)
		}
	}
	return nil
}
