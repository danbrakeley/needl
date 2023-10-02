package scraper

import (
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
