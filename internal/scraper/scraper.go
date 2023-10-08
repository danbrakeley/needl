package scraper

import (
	"fmt"
	"time"
)

type RemoteFile struct {
	Name      string
	SortName  string
	URL       string
	Timestamp time.Time // zero if unknown
	Size      int64     // -1 if unknown
}

type Scraper interface {
	ScrapeRemotes() ([]RemoteFile, error)
}

var scraperFactory = map[string]func(string, ...Option) (Scraper, error){}

// Register adds the given scraper type and that type's creation method.
func Register(typ string, createFn func(string, ...Option) (Scraper, error)) {
	scraperFactory[typ] = createFn
}

// Create looks up the given scraper type and returns a new instance of it.
func Create(typ string, opts ...Option) (Scraper, error) {
	factory, ok := scraperFactory[typ]
	if !ok {
		return nil, fmt.Errorf("type not found")
	}
	return factory(typ, opts...)
}

// ListTypeNames returns a list of all registered scraper types.
func ListTypes() []string {
	types := make([]string, 0, len(scraperFactory))
	for typ := range scraperFactory {
		types = append(types, typ)
	}
	return types
}
