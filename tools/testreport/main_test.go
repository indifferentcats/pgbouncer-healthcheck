package main

import (
	"bytes"
	"strings"
	"testing"
)

const sample = `{"Action":"start","Package":"x"}
{"Action":"build-output","ImportPath":"y [y.test]","Output":"./y.go:1: undefined: z\n"}
{"Action":"build-fail","ImportPath":"y [y.test]"}
{"Action":"run","Package":"x","Test":"TestOK"}
{"Action":"output","Package":"x","Test":"TestOK","Output":"=== RUN   TestOK\n"}
{"Action":"pass","Package":"x","Test":"TestOK"}
{"Action":"fail","Package":"x","Test":"TestBad"}
{"Action":"skip","Package":"x","Test":"TestSkip"}
not json
{"Action":"fail","Package":"x"}
{"Action":"fail","Package":"y"}
`

func TestEcho(t *testing.T) {
	var out bytes.Buffer
	if err := echo(strings.NewReader(sample), &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "./y.go:1: undefined: z\n=== RUN   TestOK\n"; got != want {
		t.Errorf("echo = %q, want %q", got, want)
	}
}

func TestSummary(t *testing.T) {
	var out bytes.Buffer
	if err := writeSummary(strings.NewReader(sample), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"| 1 | 1 | 1 | 2 |", "- `x TestBad`", "- `x (package)`", "- `y (package)`"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}
