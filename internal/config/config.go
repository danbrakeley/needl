package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	LocalPath string `toml:"path"`
	Scraper   string `toml:"scraper"`
	Threads   int    `toml:"threads"`
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open '%s': %w", path, err)
	}
	defer f.Close()

	var cfg Config
	_, err = toml.NewDecoder(f).Decode(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf("decode '%s': %w", path, err)
	}

	return cfg, nil
}
