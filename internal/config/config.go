// Package config defines tok's typed configuration: sensible defaults, an optional on-disk
// override merged over them, and a couple of environment overrides on top.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Kevalgor12/tok-proxy/internal/constants"
	"github.com/Kevalgor12/tok-proxy/internal/util"
)

// FilterLevel controls how hard `cat` strips a source file. It lives here because config
// carries the default; the filter package (ported later) reads these same values.
type FilterLevel string

const (
	FilterNone       FilterLevel = "none"
	FilterMinimal    FilterLevel = "minimal"
	FilterAggressive FilterLevel = "aggressive"
)

type ModelPricing struct {
	InputPer1k      float64 `json:"inputPer1k"`
	OutputPer1k     float64 `json:"outputPer1k"`
	CacheWritePer1k float64 `json:"cacheWritePer1k"`
	CacheReadPer1k  float64 `json:"cacheReadPer1k"`
}

type TeeConfig struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"` // failures | always | never
}

type GitFilter struct {
	DiffMaxLines int `json:"diffMaxLines"`
}

type CatFilter struct {
	MaxLines     int         `json:"maxLines"`
	DefaultLevel FilterLevel `json:"defaultLevel"`
}

type GrepFilter struct {
	MaxMatches int `json:"maxMatches"`
}

type LsFilter struct {
	MaxDepth int `json:"maxDepth"`
}

type FiltersConfig struct {
	MaxOutputLines int        `json:"maxOutputLines"`
	UltraCompact   bool       `json:"ultraCompact"`
	Git            GitFilter  `json:"git"`
	Cat            CatFilter  `json:"cat"`
	Grep           GrepFilter `json:"grep"`
	Ls             LsFilter   `json:"ls"`
}

type CacheConfig struct {
	Enabled        bool     `json:"enabled"`
	MaxEntries     int      `json:"maxEntries"`
	MaxOutputBytes int      `json:"maxOutputBytes"`
	Commands       []string `json:"commands"`
}

type Config struct {
	Version           string                  `json:"version"`
	TokenPricePer1k   float64                 `json:"tokenPricePer1k"`
	Tee               TeeConfig               `json:"tee"`
	Filters           FiltersConfig           `json:"filters"`
	Cache             CacheConfig             `json:"cache"`
	ExcludeCommands   []string                `json:"excludeCommands"`
	NoiseDirectories  []string                `json:"noiseDirectories"`
	ClaudeCodeDataDir string                  `json:"claudeCodeDataDir"`
	ModelPricing      map[string]ModelPricing `json:"modelPricing"`
}

// Defaults returns a fresh copy of the built-in configuration (fresh slices/maps, so a caller
// can't mutate a shared default).
func Defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Version:         constants.Version,
		TokenPricePer1k: 0.015,
		Tee:             TeeConfig{Enabled: true, Mode: "failures"},
		Filters: FiltersConfig{
			MaxOutputLines: 150,
			Git:            GitFilter{DiffMaxLines: 100},
			Cat:            CatFilter{MaxLines: 200, DefaultLevel: FilterMinimal},
			Grep:           GrepFilter{MaxMatches: 100},
			Ls:             LsFilter{MaxDepth: 4},
		},
		Cache: CacheConfig{
			Enabled:        true,
			MaxEntries:     5000,
			MaxOutputBytes: 65536,
			// Only idempotent, read-only command types are cached; mutating commands
			// (commit, push, install, build, test) are never served from cache.
			Commands: []string{
				"git status", "git diff", "git log", "git branch",
				"ls", "cat", "grep", "find", "json", "smart", "diff",
				"docker ps", "docker images", "kubectl get",
				"gh pr list", "gh issue list", "gh run list",
				"pip list", "npm list", "pnpm list", "yarn list",
				"env",
			},
		},
		ExcludeCommands: []string{"ssh", "vim", "nano", "less", "psql", "mysql"},
		NoiseDirectories: []string{
			"node_modules", ".git", "dist", "build", ".next", "target",
			"__pycache__", ".cache", "coverage", ".turbo", "vendor",
			".svn", ".hg", "out", "tmp", ".tmp",
		},
		ClaudeCodeDataDir: filepath.Join(home, ".claude", "projects"),
		ModelPricing: map[string]ModelPricing{
			"claude-opus-4-5":   {InputPer1k: 0.015, OutputPer1k: 0.075, CacheWritePer1k: 0.01875, CacheReadPer1k: 0.0015},
			"claude-sonnet-4-5": {InputPer1k: 0.003, OutputPer1k: 0.015, CacheWritePer1k: 0.00375, CacheReadPer1k: 0.0003},
			"claude-haiku-4-5":  {InputPer1k: 0.00025, OutputPer1k: 0.00125, CacheWritePer1k: 0.0003, CacheReadPer1k: 0.00003},
		},
	}
}

func Path() string { return filepath.Join(util.ConfigDir(), "config.json") }

// Load builds the effective config: defaults, then config.json merged over them (unmarshaling
// onto the defaults leaves absent fields untouched - a natural deep merge), then a couple of
// env overrides. It never fails; a missing or bad file just falls back to defaults.
func Load() Config {
	cfg := Defaults()
	if raw, ok := util.ReadFileIfExists(Path()); ok {
		_ = json.Unmarshal([]byte(raw), &cfg)
	} else {
		writeDefault()
	}
	if v := os.Getenv("TOK_PRICE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.TokenPricePer1k = f
		}
	}
	if os.Getenv("TOK_ULTRA_COMPACT") == "1" {
		cfg.Filters.UltraCompact = true
	}
	cfg.Version = constants.Version
	return cfg
}

func writeDefault() {
	util.EnsureDir(util.ConfigDir())
	if b, err := json.MarshalIndent(Defaults(), "", "  "); err == nil {
		_ = os.WriteFile(Path(), b, 0o644)
	}
}

func ShouldSkipTracking() bool { return os.Getenv("TOK_NO_TRACK") == "1" }
func ShouldSkipCache() bool    { return os.Getenv("TOK_NO_CACHE") == "1" }
