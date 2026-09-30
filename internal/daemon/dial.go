package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/AWDDude/loci/internal/config"
)

const (
	// spawnTimeout bounds the wait for a freshly spawned daemon to answer.
	// Starting one only opens the bbolt file and rebuilds the in-memory
	// indexes, so this is generous.
	spawnTimeout = 15 * time.Second

	// pollInterval is how often a client retries while a daemon starts or
	// stops.
	pollInterval = 50 * time.Millisecond
)

// Dial returns a connection to the daemon for cfg's database, starting one if
// none is running, and retiring one that runs a different build. The returned
// Conn carries the MCP stream.
//
// configPath is the --config flag as the caller received it, empty if not
// given. A spawned daemon is handed the same flag so it loads the same file;
// LOCI_* overrides and the XDG variables reach it through the environment.
func Dial(cfg config.Config, configPath, version string) (*Conn, error) {
	p := PathsFor(cfg)
	if err := ensureDirs(p); err != nil {
		return nil, err
	}

	// Two passes at most: the second exists only to reconnect after retiring a
	// daemon running a different build.
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := dialOrSpawn(p, configPath)
		if err != nil {
			return nil, err
		}
		if conn.Version == version {
			return conn, nil
		}
		conn.Close()
		fmt.Fprintf(os.Stderr, "loci: replacing daemon %s with %s\n", conn.Version, version)
		if err := stop(p, stopTimeout); err != nil {
			return nil, fmt.Errorf("retiring daemon %s: %w", conn.Version, err)
		}
	}
	return nil, fmt.Errorf("daemon keeps reporting a version other than %s", version)
}

// dialOrSpawn connects to a running daemon, or starts one and waits for it.
func dialOrSpawn(p Paths, configPath string) (*Conn, error) {
	if conn, err := connect(p.Socket); err == nil {
		return conn, nil
	}
	if err := spawn(p, configPath); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(spawnTimeout)
	for {
		conn, err := connect(p.Socket)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, startupError(p, err)
		}
		time.Sleep(pollInterval)
	}
}

// spawn starts a daemon if this process wins the spawn lock.
//
// Losing the race is not an error: whoever holds the lock is starting one,
// and the caller polls for it either way. This lock only keeps a burst of
// clients from starting a pile of daemons at once; the guarantee that one
// survives comes from the daemon's own lock, which is a separate file because
// this one is still held while the daemon it started is booting.
func spawn(p Paths, configPath string) error {
	lock := flock.New(p.SpawnLock)
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("taking spawn lock %s: %w", p.SpawnLock, err)
	}
	if !locked {
		return nil
	}
	defer func() { _ = lock.Unlock() }()

	// A failed connect is not proof that nobody is there: a daemon that has
	// bound but not yet written a preamble (still opening the database) times
	// out the same way. Unlinking its socket would strand it holding the
	// ownership lock with nothing able to reach it. Only a file nothing answers
	// on is the leftover of a daemon that died without cleaning up.
	if running(p.Socket) {
		return nil
	}
	if err := os.Remove(p.Socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale socket %s: %w", p.Socket, err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the loci binary: %w", err)
	}
	logFile, err := os.OpenFile(p.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening daemon log %s: %w", p.Log, err)
	}
	defer logFile.Close()

	args := []string{"daemon"}
	if configPath != "" {
		// Absolute, so the daemon's log names the file unambiguously.
		abs, err := filepath.Abs(configPath)
		if err != nil {
			return fmt.Errorf("resolving config path %s: %w", configPath, err)
		}
		args = append(args, "--config", abs)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = os.Environ()
	cmd.SysProcAttr = detachAttrs()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}
	// Deliberately not waited on: the daemon outlives this client and is
	// reparented when we exit. Its exit status reaches the user through the
	// log file, which startupError surfaces on a failed dial.
	return nil
}

// startupError turns a dial timeout into something diagnosable. A daemon that
// fails to open its database dies before it ever accepts, so the only
// evidence is in its log.
func startupError(p Paths, cause error) error {
	base := fmt.Errorf("timed out waiting for the loci daemon on %s: %w", p.Socket, cause)
	if tail := logTail(p.Log, 20); tail != "" {
		return fmt.Errorf("%w\n--- last lines of %s ---\n%s", base, p.Log, tail)
	}
	return base
}

// Proxy connects to the daemon and pipes the MCP stream between it and the
// given stdio streams, returning when either end closes. This is `loci serve`.
func Proxy(cfg config.Config, configPath, version string, stdin io.Reader, stdout io.Writer) error {
	conn, err := Dial(cfg, configPath, version)
	if err != nil {
		return err
	}
	return pipe(conn, stdin, stdout)
}
