// Package daemon lets many Loci processes share one database.
//
// bbolt locks its file to one process, and the store keeps its search indexes
// in memory, so two processes could neither share the file nor see each
// other's writes. Instead one process, the daemon, owns the database, and
// every MCP session and CLI command connects to it over a unix socket. The
// socket speaks MCP only: an MCP session is a byte pipe between stdio and the
// socket, and a CLI command is an MCP client.
//
// The daemon is spawned on demand by the first client that finds no one
// listening, so nothing has to be installed or supervised. Ported from
// engRam's daemon, which solves the same problem.
package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/AWDDude/loci/internal/config"
)

// Paths locates the daemon's runtime files. They sit beside the bbolt file
// rather than in one global location, so a config with a different db.path
// gets its own daemon instead of contending for a shared socket.
type Paths struct {
	// Dir is the directory holding the database and, normally, everything
	// below.
	Dir    string
	Socket string
	// PrivateSocketDir is set only when Socket falls back to the temp
	// directory, and names the directory that has to exist, and be ours alone,
	// before anything binds there.
	PrivateSocketDir string
	// Lock is the daemon's ownership lock, held for the daemon's whole life.
	Lock string
	// SpawnLock keeps a burst of clients from each starting a daemon. It is
	// deliberately a different file from Lock: a client holds it across the
	// spawn, and if that were the same lock the daemon it just started would
	// find its own parent holding the thing it needs and exit immediately.
	SpawnLock string
	PID       string
	Log       string
}

// maxSocketPath bounds the socket path length. A unix socket address is a
// fixed-size field in sockaddr_un (104 bytes on macOS, 108 on Linux), and
// exceeding it fails at bind with a message that says nothing about length.
// 100 leaves room under the smaller of the two.
const maxSocketPath = 100

// PathsFor derives the daemon's file locations from the configured database
// path. A path deep enough to overflow the socket address falls back to a
// digest-named socket in the temp directory; the digest keeps it unique per
// database, which is the property that matters.
func PathsFor(cfg config.Config) Paths {
	dir := filepath.Dir(cfg.DB.Path)
	privateDir := ""
	socket := filepath.Join(dir, "loci.sock")
	if len(socket) > maxSocketPath {
		// The socket is the daemon's entire access control: anything that can
		// connect can read and write every memory. A name in the shared temp
		// directory derived only from the database path is one another local
		// user could predict and bind first, so the fallback goes in a
		// per-user directory that ensureSocketDir refuses unless it is
		// owner-only.
		sum := sha256.Sum256([]byte(cfg.DB.Path))
		privateDir = filepath.Join(os.TempDir(), "loci-"+strconv.Itoa(os.Getuid()))
		socket = filepath.Join(privateDir, hex.EncodeToString(sum[:])[:12]+".sock")
	}
	return Paths{
		Dir:              dir,
		Socket:           socket,
		PrivateSocketDir: privateDir,
		Lock:             filepath.Join(dir, "daemon.lock"),
		SpawnLock:        filepath.Join(dir, "spawn.lock"),
		PID:              filepath.Join(dir, "daemon.pid"),
		Log:              filepath.Join(dir, "daemon.log"),
	}
}

// ensureDirs creates the database directory and, when the socket falls back
// to the temp directory, the private socket directory.
//
// The mode check on the fallback is the point: MkdirAll succeeds on a
// directory that already exists whoever created it, and in shared temp space
// that could be another local user's. Owner-only is what makes binding there
// safe.
func ensureDirs(p Paths) error {
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return fmt.Errorf("creating data dir %s: %w", p.Dir, err)
	}
	if p.PrivateSocketDir == "" {
		return nil
	}
	if err := os.MkdirAll(p.PrivateSocketDir, 0o700); err != nil {
		return fmt.Errorf("creating socket dir %s: %w", p.PrivateSocketDir, err)
	}
	info, err := os.Stat(p.PrivateSocketDir)
	if err != nil {
		return fmt.Errorf("checking socket dir %s: %w", p.PrivateSocketDir, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("socket dir %s is mode %#o, want owner-only access", p.PrivateSocketDir, perm)
	}
	return nil
}
