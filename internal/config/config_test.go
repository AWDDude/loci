package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points HOME and the XDG variables at a temp dir and clears any
// LOCI_* overrides from the developer's shell, so tests see only what they set.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "LOCI_") {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	home := isolate(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "share", "loci", "loci.bbolt")
	if cfg.DB.Path != want {
		t.Errorf("db.path = %q, want %q", cfg.DB.Path, want)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "loci", "config.yaml")); !os.IsNotExist(err) {
		t.Errorf("config file was created, want it left absent (stat err: %v)", err)
	}
}

func TestLoadHonorsXDGDataHome(t *testing.T) {
	isolate(t)
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(data, "loci", "loci.bbolt"); cfg.DB.Path != want {
		t.Errorf("db.path = %q, want %q", cfg.DB.Path, want)
	}
}

func TestLoadReadsFileFromXDGConfigHome(t *testing.T) {
	isolate(t)
	conf := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", conf)
	writeFile(t, filepath.Join(conf, "loci", "config.yaml"), "db:\n  path: /from/file.bbolt\n")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Path != "/from/file.bbolt" {
		t.Errorf("db.path = %q, want /from/file.bbolt", cfg.DB.Path)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	home := isolate(t)
	writeFile(t, filepath.Join(home, ".config", "loci", "config.yaml"), "db:\n  path: /from/file.bbolt\n")
	t.Setenv("LOCI_DB_PATH", "/from/env.bbolt")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Path != "/from/env.bbolt" {
		t.Errorf("db.path = %q, want /from/env.bbolt", cfg.DB.Path)
	}
}

func TestLoadExplicitPath(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "custom.yaml")
	writeFile(t, path, "db:\n  path: /explicit.bbolt\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Path != "/explicit.bbolt" {
		t.Errorf("db.path = %q, want /explicit.bbolt", cfg.DB.Path)
	}
}

func TestLoadExplicitPathMustExist(t *testing.T) {
	isolate(t)
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("want an error for a missing --config file, got nil")
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, "db:\n  pth: /typo.bbolt\n")

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "pth") {
		t.Fatalf("want an error naming the unknown key, got %v", err)
	}
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, "db: [unclosed\n")

	if _, err := Load(path); err == nil {
		t.Fatal("want a parse error, got nil")
	}
}

func TestLoadRejectsEmptyDBPath(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, "db:\n  path: \"\"\n")

	if _, err := Load(path); err == nil {
		t.Fatal("want an error for an empty db.path, got nil")
	}
}
