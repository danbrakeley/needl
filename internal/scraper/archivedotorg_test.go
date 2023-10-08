package scraper

import (
	"net/url"
	"os"
	"path"
	"testing"
)

func TestArchiveDotOrg_ScrapedCount(t *testing.T) {
	cases := []struct {
		Name          string
		ExpectedCount int
	}{
		{"images.tv.simple", 140},
		{"images.tv.full", 140},
		{"adventures.holmes.full", 126},
		{"longnames.simple", 5},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var s ArchiveDotOrg
			f, err := os.Open("testdata/" + tc.Name)
			if err != nil {
				t.Fatalf("error opening '%s': %v", tc.Name, err)
			}
			defer f.Close()

			remotes, err := s.ScrapeFromReader(f, nil)
			if err != nil {
				t.Fatalf("unexpected error in ScrapeFromReader: %v", err)
			}

			if len(remotes) != tc.ExpectedCount {
				t.Errorf("expected %d, but found %d", tc.ExpectedCount, len(remotes))
			}
		})
	}
}

func TestArchiveDotOrg_ScrapedContents(t *testing.T) {
	cases := []struct {
		FileA string
		FileB string
	}{
		{"images.tv.simple", "images.tv.full"},
	}

	for _, tc := range cases {
		t.Run(tc.FileA+"=="+tc.FileB, func(t *testing.T) {
			fa, err := os.Open("testdata/" + tc.FileA)
			if err != nil {
				t.Fatalf("error opening '%s': %v", tc.FileA, err)
			}
			defer fa.Close()
			fb, err := os.Open("testdata/" + tc.FileB)
			if err != nil {
				t.Fatalf("error opening '%s': %v", tc.FileB, err)
			}
			defer fb.Close()

			var s ArchiveDotOrg
			ra, err := s.ScrapeFromReader(fa, nil)
			if err != nil {
				t.Fatalf("unexpected error scraping '%s': %v", tc.FileA, err)
			}

			rb, err := s.ScrapeFromReader(fb, nil)
			if err != nil {
				t.Fatalf("unexpected error scraping '%s': %v", tc.FileB, err)
			}

			if len(ra) != len(rb) {
				t.Errorf("expected %d, but found %d", len(ra), len(rb))
			}

			for i := range ra {
				// Name, URL, and Timestamp will match (Size will not, and SortName is just Name)
				if ra[i].Name != rb[i].Name || ra[i].URL != rb[i].URL || ra[i].Timestamp != rb[i].Timestamp {
					t.Errorf("mismatch in entry %d: a=%v, b=%v)", i, ra[i], rb[i])
				}
			}
		})
	}
}

func TestArchiveDotOrg_SimpleLongNameTruncation(t *testing.T) {
	cases := []struct {
		File string
	}{
		{"longnames.simple"},
	}

	for _, tc := range cases {
		t.Run(tc.File, func(t *testing.T) {
			f, err := os.Open("testdata/" + tc.File)
			if err != nil {
				t.Fatalf("error opening '%s': %v", tc.File, err)
			}
			defer f.Close()

			var s ArchiveDotOrg
			r, err := s.ScrapeFromReader(f, nil)
			if err != nil {
				t.Fatalf("unexpected error scraping '%s': %v", tc.File, err)
			}

			for i := range r {
				u, err := url.Parse(r[i].URL)
				if err != nil {
					t.Fatalf("unexpected error parsing URL '%s': %v", r[i].URL, err)
				}
				if path.Base(u.Path) != r[i].Name {
					t.Errorf("expected '%s', but found '%s'", path.Base(u.Path), r[i].Name)
				}
			}
		})
	}
}
