package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Scrapers map[string]ScraperConfig `toml:"scrapers"`
}

type ScraperConfig struct {
	Type string `toml:"type"`
	URL  string `toml:"url"`
}

func LoadFromFile(path string) (Config, error) {
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
