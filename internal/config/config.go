package config

import (
	"fmt"
	"os"

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
}

type Package struct {
	Name    string   `toml:"name"`
	Version string   `toml:"version"`
	Std     string   `toml:"std"`
	Authors []string `toml:"authors"`
}

type Build struct {
	Type    string   `toml:"type"`
	Sources []string `toml:"sources"`
	Include []string `toml:"include"`
	Defines []string `toml:"defines"`
}

type Profile struct {
	Optimize string `toml:"optimize"`
	Debug    bool   `toml:"debug"`
	LTO      bool   `toml:"lto"`
}

type Dependency struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Source  string `toml:"source"`
	Path    string `toml:"path"`
	Git     string `toml:"git"`
	Rev     string `toml:"rev"`
}

type Registry struct {
	Name     string `toml:"name"`
	URL      string `toml:"url"`
	Provider string `toml:"provider"`
	Region   string `toml:"region"`
	Profile  string `toml:"profile"`
}

type Toolchain struct {
	Compiler string `toml:"compiler"`
	Minimum  string `toml:"minimum"`
	Sysroot  string `toml:"sysroot"`
}

type Legacy struct {
	Type       string   `toml:"type"`
	Root       string   `toml:"root"`
	ExtraArgs  []string `toml:"extra_args"`
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
