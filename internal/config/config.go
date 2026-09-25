// Package config loads Loci's configuration from an optional YAML file and
// LOCI_* environment variables, on top of built-in defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// DBConfig holds database settings.
type DBConfig struct {
	// Path is the bbolt file. The daemon's socket, lock, pid and log files
	// sit in the same directory, so a different path gets its own daemon.
	Path string `mapstructure:"path"`
}

// Config holds all runtime configuration for Loci.
type Config struct {
	DB DBConfig `mapstructure:"db"`
}

// Default returns the configuration used when nothing overrides it.
func Default() (Config, error) {
	dir, err := xdgDir("XDG_DATA_HOME", ".local", "share")
	if err != nil {
		return Config{}, err
	}
	return Config{
		DB: DBConfig{Path: filepath.Join(dir, "loci.bbolt")},
	}, nil
}

// DefaultPath returns where the config file is looked for when --config is
// not given.
func DefaultPath() (string, error) {
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// xdgDir returns $env/loci if env is set, otherwise ~/<fallback...>/loci.
func xdgDir(env string, fallback ...string) (string, error) {
	if base := os.Getenv(env); base != "" {
		return filepath.Join(base, "loci"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(append(append([]string{home}, fallback...), "loci")...), nil
}

// Load builds the configuration from defaults, then the config file, then
// LOCI_* environment variables, each overriding the last.
//
// explicitPath is the --config flag. When it is set the file must exist; when
// it is empty the default path is used and a missing file means defaults.
// The file is never written: it may be managed by a dotfiles tool.
func Load(explicitPath string) (Config, error) {
	cfg, err := Default()
	if err != nil {
		return Config{}, err
	}

	v := viper.New()
	v.SetConfigType("yaml")
	// Every key needs a default, even an empty one: viper only consults the
	// environment for keys it already knows about.
	v.SetDefault("db.path", cfg.DB.Path)
	v.SetEnvPrefix("LOCI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	path := explicitPath
	if path == "" {
		if path, err = DefaultPath(); err != nil {
			return Config{}, err
		}
	}
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if explicitPath != "" || !errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("reading config %s: %w", path, err)
		}
	}

	// Exact, so a misspelled key is reported instead of silently ignored.
	if err := v.UnmarshalExact(&cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if cfg.DB.Path == "" {
		return Config{}, fmt.Errorf("config %s: db.path must not be empty", path)
	}
	return cfg, nil
}
