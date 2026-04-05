package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Package      Package            `toml:"package"`
	Build        Build              `toml:"build"`
	Profiles     map[string]Profile `toml:"profile"`
	Dependencies []Dependency       `toml:"dependencies"`
	Registries   []Registry         `toml:"registry"`
	Toolchain    Toolchain          `toml:"toolchain"`
	Legacy       *Legacy            `toml:"legacy"`
	Workspace    *Workspace         `toml:"workspace"`
	Hooks        *Hooks             `toml:"hooks"`
}

type Package struct {
	Name    string   `toml:"name"`
	Version string   `toml:"version"`
	Std     string   `toml:"std"`
	Authors []string `toml:"authors"`
}

type Build struct {
	Type    string     `toml:"type"`
	Sources []string   `toml:"sources"`
	Include []string   `toml:"include"`
	Defines []string   `toml:"defines"`
	Flags   BuildFlags `toml:"flags"`
}

type BuildFlags struct {
	CXXFlags  []string `toml:"cxx_flags"`
	LinkFlags []string `toml:"link_flags"`
	Defines   []string `toml:"defines"`
}

type Profile struct {
	Optimize string `toml:"optimize"`
	Debug    bool   `toml:"debug"`
	LTO      bool   `toml:"lto"`
}

type Dependency struct {
	Name      string `toml:"name"`
	Version   string `toml:"version"`
	Source    string `toml:"source"`
	BuildType string `toml:"build_type"`
	Path      string `toml:"path"`
	Git       string `toml:"git"`
	Rev       string `toml:"rev"`
}

// IsLocal returns true if this dependency uses a local source path.
func (d Dependency) IsLocal() bool {
	return d.Source == "local"
}

type Registry struct {
	Name        string `toml:"name"`
	URL         string `toml:"url"`
	Provider    string `toml:"provider"`
	Region      string `toml:"region"`
	Profile     string `toml:"profile"`
	EndpointURL string `toml:"endpoint_url"`
	AccessKey   string `toml:"access_key"`
	SecretKey   string `toml:"secret_key"`
}

type Toolchain struct {
	Compiler string `toml:"compiler"`
	Minimum  string `toml:"minimum"`
	Sysroot  string `toml:"sysroot"`
}

type Legacy struct {
	Type      string   `toml:"type"`
	Root      string   `toml:"root"`
	ExtraArgs []string `toml:"extra_args"`
}

type Workspace struct {
	Members []string `toml:"members"`
}

type Hooks struct {
	PreBuild    string `toml:"pre-build"`
	PostBuild   string `toml:"post-build"`
	PrePublish  string `toml:"pre-publish"`
	PostPublish string `toml:"post-publish"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func (c *Config) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(c); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// Global is the user-level configuration stored at ~/.cstow/config.toml.
type Global struct {
	Defaults     GlobalDefaults  `toml:"defaults"`
	Cache        GlobalCache     `toml:"cache"`
	Repositories []RepoSource    `toml:"repositories"`
	Registries   []Registry      `toml:"registry"`
	Toolchain    GlobalToolchain `toml:"toolchain"`
	Build        GlobalBuild     `toml:"build"`
	Network      GlobalNetwork   `toml:"network"`
}

type GlobalDefaults struct {
	Std     string `toml:"std"`
	Profile string `toml:"profile"`
	Jobs    int    `toml:"jobs"`
	Color   bool   `toml:"color"`
}

type GlobalCache struct {
	Dir           string `toml:"dir"`
	MaxSizeGB     int    `toml:"max_size_gb"`
	RetentionDays int    `toml:"retention_days"`
}

type RepoSource struct {
	Name       string `toml:"name"`
	Path       string `toml:"path"`
	Git        string `toml:"git"`
	Branch     string `toml:"branch"`
	AutoUpdate bool   `toml:"auto_update"`
	Archive    string `toml:"archive"`
	Priority   int    `toml:"priority"`
}

type GlobalToolchain struct {
	Prefer   string `toml:"prefer"`
	MinGCC   string `toml:"min_gcc"`
	MinClang string `toml:"min_clang"`
}

type GlobalBuild struct {
	Flags GlobalBuildFlags `toml:"flags"`
}

type GlobalBuildFlags struct {
	CXXFlags  []string `toml:"cxx_flags"`
	LinkFlags []string `toml:"link_flags"`
	Defines   []string `toml:"defines"`
}

type GlobalNetwork struct {
	Proxy   string   `toml:"proxy"`
	NoProxy []string `toml:"no_proxy"`
	Timeout int      `toml:"timeout_sec"`
	Retries int      `toml:"retries"`
}

// LoadGlobal reads ~/.cstow/config.toml. Returns a zero-value Global with
// sensible defaults if the file does not exist.
func LoadGlobal() (*Global, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	path := filepath.Join(home, ".cstow", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			g := defaultGlobal()
			return g, nil
		}
		return nil, fmt.Errorf("read global config: %w", err)
	}
	var g Global
	if err := toml.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parse global config: %w", err)
	}
	applyDefaults(&g)
	return &g, nil
}

func defaultGlobal() *Global {
	g := &Global{
		Defaults: GlobalDefaults{Std: "c++17", Profile: "debug"},
		Cache:    GlobalCache{Dir: "~/.cstow/cache"},
	}
	return g
}

func applyDefaults(g *Global) {
	if g.Defaults.Std == "" {
		g.Defaults.Std = "c++17"
	}
	if g.Defaults.Profile == "" {
		g.Defaults.Profile = "debug"
	}
	if g.Cache.Dir == "" {
		g.Cache.Dir = "~/.cstow/cache"
	}
}

// ResolvePrimaryRegistry picks the effective registry configuration for commands
// that operate on a single registry.
func ResolvePrimaryRegistry(projectRegistries []Registry, global *Global) (Registry, error) {
	if len(projectRegistries) > 0 {
		reg := projectRegistries[0]
		if global == nil || len(global.Registries) == 0 {
			return reg, nil
		}
		if fallback, ok := matchRegistry(reg, global.Registries); ok {
			return mergeRegistry(reg, fallback), nil
		}
		return reg, nil
	}

	if global != nil && len(global.Registries) > 0 {
		return global.Registries[0], nil
	}

	return Registry{}, fmt.Errorf("no registry configured")
}

func matchRegistry(target Registry, candidates []Registry) (Registry, bool) {
	if target.Name != "" {
		for _, candidate := range candidates {
			if candidate.Name == target.Name {
				return candidate, true
			}
		}
	}
	if target.URL != "" {
		for _, candidate := range candidates {
			if candidate.URL == target.URL {
				return candidate, true
			}
		}
	}
	return Registry{}, false
}

func mergeRegistry(primary, fallback Registry) Registry {
	merged := primary
	if merged.Name == "" {
		merged.Name = fallback.Name
	}
	if merged.URL == "" {
		merged.URL = fallback.URL
	}
	if merged.Provider == "" {
		merged.Provider = fallback.Provider
	}
	if merged.Region == "" {
		merged.Region = fallback.Region
	}
	if merged.Profile == "" {
		merged.Profile = fallback.Profile
	}
	if merged.EndpointURL == "" {
		merged.EndpointURL = fallback.EndpointURL
	}
	if merged.AccessKey == "" {
		merged.AccessKey = fallback.AccessKey
	}
	if merged.SecretKey == "" {
		merged.SecretKey = fallback.SecretKey
	}
	return merged
}

// RepositoryPaths returns ordered directory paths for Finder, expanding ~ and
// sorting by Priority (lower = higher priority). The built-in
// ~/.cstow/repository/ is always appended last as a fallback.
func (g *Global) RepositoryPaths() []string {
	home, _ := os.UserHomeDir()

	sorted := make([]RepoSource, len(g.Repositories))
	copy(sorted, g.Repositories)
	// stable sort by priority (default 50), lower first
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			pj := sorted[j].Priority
			pj1 := sorted[j-1].Priority
			if pj == 0 {
				pj = 50
			}
			if pj1 == 0 {
				pj1 = 50
			}
			if pj < pj1 {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			}
		}
	}

	paths := make([]string, 0, len(sorted)+1)
	for _, r := range sorted {
		if r.Path != "" {
			p := r.Path
			if len(p) >= 2 && p[:2] == "~/" {
				p = filepath.Join(home, p[2:])
			}
			paths = append(paths, p)
		}
	}
	// built-in fallback
	paths = append(paths, filepath.Join(home, ".cstow", "repository"))
	return paths
}
