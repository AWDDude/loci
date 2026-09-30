package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AWDDude/loci/internal/model"
	"github.com/AWDDude/loci/internal/service"
)

// These tests run a built loci binary against a real daemon: spawning, the
// stdio pipe, signals, and retiring an old build can only be checked from
// outside the test process.

// shortDir makes a temp directory with a short path, so the daemon's socket
// sits beside the database as in a real install rather than on the
// length fallback.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lci")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func buildLoci(t *testing.T, version string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "loci-"+version)
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building loci: %v\n%s", err, out)
	}
	return bin
}

// testEnv isolates a run from the developer's own config and data, and points
// it at a fresh database. The daemon it spawns is stopped at cleanup.
func testEnv(t *testing.T, bin string) (env []string, dbPath string) {
	t.Helper()
	dir := shortDir(t)
	dbPath = filepath.Join(dir, "loci.bbolt")
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "LOCI_") || strings.HasPrefix(name, "XDG_") || name == "HOME" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+dir, "LOCI_DB_PATH="+dbPath)
	t.Cleanup(func() {
		cmd := exec.Command(bin, "daemon", "stop")
		cmd.Env = env
		_ = cmd.Run()
	})
	return env, dbPath
}

func run(t *testing.T, bin string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("loci %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return string(out)
}

func runJSON(t *testing.T, bin string, env []string, v any, args ...string) {
	t.Helper()
	out := run(t, bin, env, append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("decoding %q: %v", out, err)
	}
}

// session is a `loci serve` process driven over its stdio, as an MCP client
// would drive it.
type session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	nextID int
}

func startSession(t *testing.T, bin string, env []string) *session {
	t.Helper()
	cmd := exec.Command(bin, "serve")
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &session{cmd: cmd, stdin: stdin, lines: make(chan string, 16)}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
		close(s.lines)
	}()
	t.Cleanup(func() {
		stdin.Close()
		_ = cmd.Wait()
	})

	s.request(t, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "0"},
	})
	s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return s
}

func (s *session) send(t *testing.T, msg any) {
	t.Helper()
	data, _ := json.Marshal(msg)
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		t.Fatalf("writing to session: %v", err)
	}
}

func (s *session) request(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	s.nextID++
	id := s.nextID
	s.send(t, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	timeout := time.After(10 * time.Second)
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatalf("session closed while waiting for %s", method)
			}
			var resp struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(line), &resp) != nil || resp.ID != id {
				continue
			}
			if resp.Error != nil {
				t.Fatalf("%s: %s", method, resp.Error)
			}
			return resp.Result
		case <-timeout:
			t.Fatalf("no reply to %s", method)
		}
	}
}

func (s *session) callTool(t *testing.T, name string, args map[string]any, out any) {
	t.Helper()
	raw := s.request(t, "tools/call", map[string]any{"name": name, "arguments": args})
	var res struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || res.IsError {
		t.Fatalf("%s: %s", name, raw)
	}
	if err := json.Unmarshal(res.StructuredContent, out); err != nil {
		t.Fatal(err)
	}
}

func TestCLIAndSessionsShareOneDaemon(t *testing.T) {
	bin := buildLoci(t, "v1")
	env, _ := testEnv(t, bin)

	// The first CLI command spawns the daemon.
	var search service.EntitySearchResult
	runJSON(t, bin, env, &search, "entity", "search", "loci")
	var loci model.Entity
	runJSON(t, bin, env, &loci, "entity", "create", search.Placeholder,
		"--type", "project", "--name", "Loci", "--description", "entity memory server")

	// An MCP session sees what the CLI wrote...
	s := startSession(t, bin, env)
	var got service.EntitySearchResult
	s.callTool(t, "entity_search", map[string]any{"query": "loci"}, &got)
	if len(got.Entities) != 1 || got.Entities[0].Entity.ID != loci.ID {
		t.Fatalf("session search = %+v", got)
	}

	// ...and the CLI sees what the session wrote.
	var ms service.MemorySearchResult
	s.callTool(t, "memory_search", map[string]any{"query": "bbolt"}, &ms)
	var m service.MemoryDetail
	s.callTool(t, "memory_create", map[string]any{
		"placeholder": ms.Placeholder, "title": "uses bbolt", "content": "bbolt over sqlite",
		"links": []map[string]any{{"type": "decision", "entity": loci.ID}},
	}, &m)
	out := run(t, bin, env, "entity", "get", loci.ID)
	if !strings.Contains(out, "decision  uses bbolt") {
		t.Fatalf("CLI does not see the session's memory:\n%s", out)
	}

	var st struct {
		Running bool   `json:"running"`
		Version string `json:"version"`
	}
	runJSON(t, bin, env, &st, "daemon", "status")
	if !st.Running || st.Version != "v1" {
		t.Fatalf("status = %+v", st)
	}
}

func TestDaemonStopEndsAttachedSessions(t *testing.T) {
	bin := buildLoci(t, "v1")
	env, dbPath := testEnv(t, bin)
	s := startSession(t, bin, env)

	if out := run(t, bin, env, "daemon", "stop"); !strings.Contains(out, "Stopped") {
		t.Fatalf("stop output: %q", out)
	}
	// The session's stdout closes once the daemon hangs up on it.
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				if _, err := os.Stat(filepath.Join(filepath.Dir(dbPath), "loci.sock")); !os.IsNotExist(err) {
					t.Error("socket survived daemon stop")
				}
				return
			}
		case <-deadline:
			t.Fatal("session still open after daemon stop")
		}
	}
}

func TestClientRetiresADaemonRunningADifferentBuild(t *testing.T) {
	old := buildLoci(t, "v1")
	env, _ := testEnv(t, old)
	run(t, old, env, "entity", "search", "x")

	// An upgraded binary finds the old daemon, stops it and starts its own.
	upgraded := buildLoci(t, "v2")
	run(t, upgraded, env, "entity", "search", "x")
	var st struct {
		Version string `json:"version"`
	}
	runJSON(t, upgraded, env, &st, "daemon", "status")
	if st.Version != "v2" {
		t.Fatalf("daemon version after upgrade = %q, want v2", st.Version)
	}
	// Stop the replacement with its own binary; the cleanup's v1 stop then
	// finds nothing running.
	run(t, upgraded, env, "daemon", "stop")
}

func TestSpawnedDaemonUsesTheConfigFlag(t *testing.T) {
	bin := buildLoci(t, "v1")
	env, _ := testEnv(t, bin)
	// Drop LOCI_DB_PATH so only the config file names the database.
	var noOverride []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "LOCI_DB_PATH=") {
			noOverride = append(noOverride, kv)
		}
	}
	dir := shortDir(t)
	dbPath := filepath.Join(dir, "from-config.bbolt")
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, fmt.Appendf(nil, "db:\n  path: %s\n", dbPath), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd := exec.Command(bin, "--config", cfgPath, "daemon", "stop")
		cmd.Env = noOverride
		_ = cmd.Run()
	})

	run(t, bin, noOverride, "--config", cfgPath, "entity", "search", "x")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("the spawned daemon did not open the configured database: %v", err)
	}
	var st struct {
		Running  bool   `json:"running"`
		Database string `json:"database"`
	}
	runJSON(t, bin, noOverride, &st, "--config", cfgPath, "daemon", "status")
	if !st.Running || st.Database != dbPath {
		t.Fatalf("status = %+v, want running on %s", st, dbPath)
	}
}
