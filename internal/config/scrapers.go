package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Scrapers map[string]Scraper

type Scraper struct {
	Type string `toml:"type"`
	URL  string `toml:"url"`
}

func LoadScrapers(path string) (Scrapers, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open '%s': %w", path, err)
	}
	defer f.Close()

	var scrapers Scrapers
	_, err = toml.NewDecoder(f).Decode(&scrapers)
	if err != nil {
		return nil, fmt.Errorf("decode '%s': %w", path, err)
	}

	return scrapers, nil
}
