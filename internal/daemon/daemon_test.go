package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/AWDDude/loci/internal/config"
)

// These tests run the daemon inside the test process. That exercises the real
// listener, accept loop and MCP wiring, but it means the pid file names the
// test binary, so nothing here may call Stop: it would signal the test run
// itself. Shutdown is driven through the context instead; spawning, stopping
// by signal and retiring an old build are covered end-to-end by the CLI
// integration test, which runs a built binary.

// testConfig points at a short-pathed temp directory so the daemon's files
// land beside the database, the way they do in a real install. t.TempDir() is
// deep enough on macOS to push the socket onto its length fallback, which
// would quietly test a different layout than the one users get.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{DB: config.DBConfig{Path: filepath.Join(shortTempDir(t), "loci.bbolt")}}
}

// shortTempDir makes a temp directory with a short path, removed at the end
// of the test.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lc")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// echoMCP builds an MCP server with one tool that reports which daemon
// answered, so a test can tell two daemons apart. The returned counters
// report how many times the server was built and cleaned up.
func echoMCP(name string) (NewMCPFunc, *int, *int) {
	builds, cleanups := 0, 0
	return func() (*mcpserver.MCPServer, func(), error) {
		builds++
		s := mcpserver.NewMCPServer("loci-test", "1.0.0", mcpserver.WithToolCapabilities(false))
		s.AddTool(
			mcp.NewTool("echo", mcp.WithString("text", mcp.Required())),
			func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return mcp.NewToolResultText(name + ":" + req.GetString("text", "")), nil
			},
		)
		return s, func() { cleanups++ }, nil
	}, &builds, &cleanups
}

// startDaemon runs Serve in the background and waits for it to accept
// connections. The returned stop func cancels it and reports what Serve
// returned.
func startDaemon(t *testing.T, cfg config.Config, version string, newMCP NewMCPFunc, idle time.Duration) (Paths, func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(ctx, cfg, version, newMCP, idle) }()

	p := PathsFor(cfg)
	waitFor(t, 10*time.Second, func() bool { return running(p.Socket) })

	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		select {
		case err := <-errCh:
			return err
		case <-time.After(10 * time.Second):
			return fmt.Errorf("daemon did not shut down")
		}
	}
	t.Cleanup(func() { stop() })
	return p, stop
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// dialClient connects to the daemon and completes the MCP handshake over the
// socket with mcp-go's client, which is how the CLI will talk to it.
func dialClient(t *testing.T, socket string) *client.Client {
	t.Helper()
	conn, err := connect(socket)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	c := client.NewClient(transport.NewIO(conn, conn, nil))
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "0"}
	if _, err := c.Initialize(ctx, init); err != nil {
		t.Fatalf("mcp handshake: %v", err)
	}
	return c
}

func echo(c *client.Client, text string) (string, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = "echo"
	req.Params.Arguments = map[string]any{"text": text}
	res, err := c.CallTool(context.Background(), req)
	if err != nil {
		return "", err
	}
	return res.Content[0].(mcp.TextContent).Text, nil
}

func TestPathsFor_LivesBesideTheDatabase(t *testing.T) {
	cfg := testConfig(t)
	p := PathsFor(cfg)
	dir := filepath.Dir(cfg.DB.Path)

	// One daemon per database directory is what lets a config with its own
	// db.path run its own daemon instead of contending for one socket.
	for name, got := range map[string]string{
		"socket": p.Socket, "lock": p.Lock, "spawn lock": p.SpawnLock, "pid": p.PID, "log": p.Log,
	} {
		if filepath.Dir(got) != dir {
			t.Errorf("%s path %q is not in the database directory %q", name, got, dir)
		}
	}
	if filepath.Base(p.Socket) != "loci.sock" {
		t.Errorf("socket = %q, want loci.sock", p.Socket)
	}
}

func TestPathsFor_FallsBackWhenTheSocketPathIsTooLong(t *testing.T) {
	// A sockaddr_un path is a fixed-size field (104 bytes on macOS), and
	// overflowing it fails at bind with nothing that mentions length.
	deep := filepath.Join(t.TempDir(), strings.Repeat("a-fairly-long-directory-name/", 10))
	cfg := config.Config{DB: config.DBConfig{Path: filepath.Join(deep, "loci.bbolt")}}

	p := PathsFor(cfg)
	if len(p.Socket) > maxSocketPath {
		t.Errorf("fallback socket path is still %d bytes: %q", len(p.Socket), p.Socket)
	}
	if filepath.Dir(p.Socket) == deep {
		t.Errorf("expected the fallback to leave the database directory, got %q", p.Socket)
	}
	if filepath.Dir(p.Lock) != deep {
		t.Errorf("lock should stay beside the database, got %q", p.Lock)
	}

	other := config.Config{DB: config.DBConfig{Path: filepath.Join(deep, "other.bbolt")}}
	if PathsFor(other).Socket == p.Socket {
		t.Error("two databases produced the same fallback socket")
	}
}

func TestEnsureDirs_RefusesAFallbackDirectoryOthersCanWrite(t *testing.T) {
	deep := filepath.Join(t.TempDir(), strings.Repeat("a-fairly-long-directory-name/", 10))
	p := PathsFor(config.Config{DB: config.DBConfig{Path: filepath.Join(deep, "loci.bbolt")}})
	if p.PrivateSocketDir == "" {
		t.Fatal("the length fallback left the socket in shared temp space")
	}
	if err := ensureDirs(p); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	info, err := os.Stat(p.PrivateSocketDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("created the fallback directory as %#o, want 0700", perm)
	}

	// A directory in shared temp space that another local user can write to is
	// one where they can bind this predictable path first.
	if err := os.Chmod(p.PrivateSocketDir, 0o777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p.PrivateSocketDir, 0o700)
	if err := ensureDirs(p); err == nil {
		t.Error("accepted a world-writable fallback socket directory")
	}
}

func TestServe_CarriesMCPToConcurrentClients(t *testing.T) {
	cfg := testConfig(t)
	newMCP, _, _ := echoMCP("daemon")
	p, stop := startDaemon(t, cfg, "1.0.0", newMCP, 0)

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := dialClient(t, p.Socket)
			want := fmt.Sprintf("daemon:hello-%d", i)
			got, err := echo(c, fmt.Sprintf("hello-%d", i))
			if err != nil || got != want {
				t.Errorf("client %d got %q, %v; want %q", i, got, err, want)
			}
		}()
	}
	wg.Wait()
	if err := stop(); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestServe_ProbesDoNotLogAsFailures(t *testing.T) {
	var logs strings.Builder
	var mu sync.Mutex
	log.SetOutput(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return logs.Write(p)
	}))
	defer log.SetOutput(os.Stderr)

	cfg := testConfig(t)
	newMCP, _, _ := echoMCP("daemon")
	p, stop := startDaemon(t, cfg, "1.0.0", newMCP, 0)
	// running() dials and hangs up without reading, as `loci daemon status`
	// and the spawn path's liveness check do.
	for range 5 {
		running(p.Socket)
	}
	time.Sleep(100 * time.Millisecond)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(logs.String(), "session ended") {
		t.Errorf("a probe was logged as a failed session:\n%s", logs.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestServe_ReportsItsVersionInThePreamble(t *testing.T) {
	cfg := testConfig(t)
	newMCP, _, _ := echoMCP("daemon")
	p, _ := startDaemon(t, cfg, "2.3.0", newMCP, 0)

	conn, err := connect(p.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// This is what tells a client upgraded underneath a running daemon that it
	// is talking to the old build.
	if conn.Version != "2.3.0" {
		t.Errorf("preamble version = %q, want 2.3.0", conn.Version)
	}
}

func TestServe_SecondDaemonYieldsToTheOneThatOwnsTheDatabase(t *testing.T) {
	cfg := testConfig(t)
	first, _, _ := echoMCP("first")
	p, _ := startDaemon(t, cfg, "1.0.0", first, 0)

	// The loser of a spawn race must exit quietly, without opening the
	// database or touching the socket.
	second, secondBuilds, _ := echoMCP("second")
	if err := Serve(context.Background(), cfg, "1.0.0", second, 0); err != nil {
		t.Fatalf("second daemon should exit cleanly, got %v", err)
	}
	if *secondBuilds != 0 {
		t.Error("the losing daemon opened the database instead of exiting first")
	}
	if got, err := echo(dialClient(t, p.Socket), "x"); err != nil || got != "first:x" {
		t.Errorf("got %q, %v; want the original daemon still serving", got, err)
	}
}

func TestServe_ReplacesASocketLeftByACrashedDaemon(t *testing.T) {
	cfg := testConfig(t)
	p := PathsFor(cfg)
	// A unix socket file outlives the process that bound it: it refuses
	// connections but still blocks bind.
	if err := os.WriteFile(p.Socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	newMCP, _, _ := echoMCP("daemon")
	startDaemon(t, cfg, "1.0.0", newMCP, 0)
	if _, err := echo(dialClient(t, p.Socket), "x"); err != nil {
		t.Errorf("echo after replacing a stale socket: %v", err)
	}
}

func TestListen_RefusesToStealALiveSocket(t *testing.T) {
	p := PathsFor(testConfig(t))
	listener, err := net.Listen("unix", p.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	if _, err := listen(p.Socket); err == nil {
		t.Error("listen took over a socket that is being served")
	}
}

func TestServe_ExitsAfterSittingIdle(t *testing.T) {
	cfg := testConfig(t)
	newMCP, _, cleanups := echoMCP("daemon")

	// A daemon spawned by a client that then gave up must not linger.
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), cfg, "1.0.0", newMCP, 200*time.Millisecond) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("idle shutdown returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not shut down when idle")
	}
	if *cleanups != 1 {
		t.Errorf("cleanup ran %d times, want 1", *cleanups)
	}
	p := PathsFor(cfg)
	for _, path := range []string{p.Socket, p.PID} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s survived shutdown", path)
		}
	}
}

func TestServe_StaysUpWhileAClientIsAttached(t *testing.T) {
	cfg := testConfig(t)
	newMCP, _, _ := echoMCP("daemon")
	idle := 200 * time.Millisecond
	p, _ := startDaemon(t, cfg, "1.0.0", newMCP, idle)

	c := dialClient(t, p.Socket)
	// An idle session still holds its connection, which must keep the timer
	// from firing: a long pause between tool calls is normal.
	time.Sleep(4 * idle)
	if _, err := echo(c, "still here"); err != nil {
		t.Fatalf("echo after idling: %v", err)
	}
}

func TestQuery_ReportsTheRunningDaemon(t *testing.T) {
	cfg := testConfig(t)
	if Query(cfg).Running {
		t.Error("reported a daemon before one was started")
	}

	newMCP, _, _ := echoMCP("daemon")
	_, stop := startDaemon(t, cfg, "4.5.6", newMCP, 0)
	st := Query(cfg)
	if !st.Running || st.Version != "4.5.6" || st.PID != os.Getpid() || st.Database != cfg.DB.Path {
		t.Fatalf("status = %+v", st)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if Query(cfg).Running {
		t.Error("still reporting a daemon after shutdown")
	}
}

func TestSpawnLock_OnlyOneCallerSpawns(t *testing.T) {
	p := PathsFor(testConfig(t))
	if err := ensureDirs(p); err != nil {
		t.Fatal(err)
	}
	lock, held, err := takeLock(p.SpawnLock)
	if err != nil || !held {
		t.Fatalf("takeLock: held=%v err=%v", held, err)
	}
	defer lock.Unlock()

	if err := spawn(p, ""); err != nil {
		t.Errorf("spawn should yield quietly when the lock is held, got %v", err)
	}
	if _, err := os.Stat(p.Log); !os.IsNotExist(err) {
		t.Error("spawn started a daemon despite losing the lock")
	}
}

func TestSpawnLock_IsNotTheDaemonsOwnershipLock(t *testing.T) {
	p := PathsFor(testConfig(t))
	if err := ensureDirs(p); err != nil {
		t.Fatal(err)
	}
	// The client holds the spawn lock while the daemon it started is booting.
	// If they were one file, that daemon would find the lock held by its own
	// parent and exit immediately.
	spawnLock, held, err := takeLock(p.SpawnLock)
	if err != nil || !held {
		t.Fatalf("taking the spawn lock: held=%v err=%v", held, err)
	}
	defer spawnLock.Unlock()

	daemonLock, held, err := takeLock(p.Lock)
	if err != nil || !held {
		t.Fatalf("a daemon could not take its lock while a client held the spawn lock: held=%v err=%v", held, err)
	}
	daemonLock.Unlock()
}

func TestSpawn_LeavesASocketThatStillAnswersAlone(t *testing.T) {
	p := PathsFor(testConfig(t))
	if err := ensureDirs(p); err != nil {
		t.Fatal(err)
	}
	// A daemon that has bound but not yet written its preamble looks like a
	// dead socket to connect. Unlinking it would strand a daemon that holds
	// the database and the ownership lock.
	listener, err := net.Listen("unix", p.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	if err := spawn(p, ""); err != nil {
		t.Fatal(err)
	}
	if !running(p.Socket) {
		t.Error("spawn unlinked a socket that was still answering")
	}
	if _, err := os.Stat(p.Log); !os.IsNotExist(err) {
		t.Error("spawn started a daemon despite one already being up")
	}
}

func TestStop_TreatsAMissingPIDFileAsAlreadyStopped(t *testing.T) {
	// After an upgrade two clients can both retire the same old daemon. The
	// winner's daemon unlinks the pid file on its way out, so the loser must
	// not fail on a file that is gone precisely because the work is done.
	p := PathsFor(testConfig(t))
	if err := ensureDirs(p); err != nil {
		t.Fatal(err)
	}
	if err := stop(p, stopTimeout); err != nil {
		t.Errorf("stop with no pid file and nothing listening: %v", err)
	}
}

func TestStop_WaitsForTheOwnershipLockToBeReleased(t *testing.T) {
	cfg := testConfig(t)
	newMCP, _, _ := echoMCP("daemon")
	p, shutdown := startDaemon(t, cfg, "1.0.0", newMCP, 0)
	if err := shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := waitForLockFree(p, stopTimeout); err != nil {
		t.Fatal(err)
	}
	lock, held, err := takeLock(p.Lock)
	if err != nil || !held {
		t.Fatalf("the ownership lock is still held after the daemon stopped: held=%v err=%v", held, err)
	}
	lock.Unlock()
}

func TestConnTracker_TurnsAwayAClientOnceIdleShutdownBegins(t *testing.T) {
	fired := make(chan struct{})
	tracker := newConnTracker(10*time.Millisecond, func() { close(fired) })
	tracker.arm()
	<-fired
	// Nothing has been written to a connection accepted this late, so the only
	// safe answer is to drop it: the client retries and spawns a replacement.
	if tracker.add() {
		t.Error("accepted a connection after idle shutdown began")
	}
}

func TestConnTracker_DoesNotShutDownOnAClientThatBeatTheTimer(t *testing.T) {
	// time.Timer.Stop reports false once the callback has started, so the
	// callback has to re-check the count itself.
	tracker := newConnTracker(time.Hour, func() {
		t.Error("idle shutdown fired with a client attached")
	})
	if !tracker.add() {
		t.Fatal("add refused the first connection")
	}
	tracker.fire()
	if tracker.stopped {
		t.Error("the tracker stopped despite an attached client")
	}
}
