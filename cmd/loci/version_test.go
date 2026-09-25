package main

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func runRoot(t *testing.T, args ...string) string {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("loci %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func TestVersionHuman(t *testing.T) {
	got := runRoot(t, "version")
	want := "loci " + version + " (" + runtime.GOOS + "/" + runtime.GOARCH
	if !strings.HasPrefix(got, want) {
		t.Errorf("output = %q, want prefix %q", got, want)
	}
}

func TestVersionJSON(t *testing.T) {
	var info versionInfo
	if err := json.Unmarshal([]byte(runRoot(t, "version", "--json")), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version != version || info.OS != runtime.GOOS || info.Go != runtime.Version() {
		t.Errorf("got %+v", info)
	}
}
