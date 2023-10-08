package main

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/danbrakeley/needl/internal/scraper"
)

func Test_DiffFiles(t *testing.T) {
	cases := []struct {
		Name            string
		Locals          []LocalFile
		Remotes         []scraper.RemoteFile
		ExpectedExtra   []LocalFile
		ExpectedMissing []scraper.RemoteFile
		ExpectedChanged []scraper.RemoteFile
	}{
		{
			"single file match",
			[]LocalFile{localFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]LocalFile{},
			[]scraper.RemoteFile{},
			[]scraper.RemoteFile{},
		},
		{
			"single file extra",
			[]LocalFile{localFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]scraper.RemoteFile{},
			[]LocalFile{localFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]scraper.RemoteFile{},
			[]scraper.RemoteFile{},
		},
		{
			"single file missing",
			[]LocalFile{},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]LocalFile{},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]scraper.RemoteFile{},
		},
		{
			"single file no remote size",
			[]LocalFile{localFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-01-01 00:00", -1)},
			[]LocalFile{},
			[]scraper.RemoteFile{},
			[]scraper.RemoteFile{},
		},
		{
			"single file changed size",
			[]LocalFile{localFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-01-01 00:00", 52345)},
			[]LocalFile{},
			[]scraper.RemoteFile{},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-01-01 00:00", 52345)},
		},
		{
			"single file changed time",
			[]LocalFile{localFile(t, "foo", "2020-01-01 00:00", 1234)},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-02-04 02:10", 1234)},
			[]LocalFile{},
			[]scraper.RemoteFile{},
			[]scraper.RemoteFile{remoteFile(t, "foo", "2020-02-04 02:10", 1234)},
		},
		{
			"multi files match",
			[]LocalFile{
				localFile(t, "foo", "2020-01-01 00:00", 1234),
				localFile(t, "pool", "2020-02-03 01:02", 444),
			},
			[]scraper.RemoteFile{
				remoteFile(t, "foo", "2020-01-01 00:00", 1234),
				remoteFile(t, "pool", "2020-02-03 01:02", 444),
			},
			[]LocalFile{},
			[]scraper.RemoteFile{},
			[]scraper.RemoteFile{},
		},
		{
			"multi files missing size",
			[]LocalFile{
				localFile(t, "foo", "2020-01-01 00:00", 1234),
				localFile(t, "pool", "2020-02-03 01:02", 444),
			},
			[]scraper.RemoteFile{
				remoteFile(t, "foo", "2020-01-01 00:00", -1),
				remoteFile(t, "pool", "2020-02-03 01:02", -1),
			},
			[]LocalFile{},
			[]scraper.RemoteFile{},
			[]scraper.RemoteFile{},
		},
		{
			"multi files extras, missing, changed",
			[]LocalFile{
				localFile(t, "foo", "2020-01-01 00:00", 1234),
				localFile(t, "pool", "2020-02-03 01:02", 444),
				localFile(t, "stand", "2021-12-31 23:59", 3548),
			},
			[]scraper.RemoteFile{
				remoteFile(t, "foo", "2020-01-01 00:00", -1),
				remoteFile(t, "pool", "2020-10-01 19:28", -1),
				remoteFile(t, "zero", "2000-01-01 00:00", -1),
			},
			[]LocalFile{localFile(t, "stand", "2021-12-31 23:59", 3548)},
			[]scraper.RemoteFile{remoteFile(t, "zero", "2000-01-01 00:00", -1)},
			[]scraper.RemoteFile{remoteFile(t, "pool", "2020-10-01 19:28", -1)},
		},
		{
			"multi remote files, no local",
			[]LocalFile{},
			[]scraper.RemoteFile{
				remoteFile(t, "foo", "2020-01-01 00:00", -1),
				remoteFile(t, "pool", "2020-10-01 19:28", -1),
				remoteFile(t, "zero", "2000-01-01 00:00", -1),
			},
			[]LocalFile{},
			[]scraper.RemoteFile{
				remoteFile(t, "foo", "2020-01-01 00:00", -1),
				remoteFile(t, "pool", "2020-10-01 19:28", -1),
				remoteFile(t, "zero", "2000-01-01 00:00", -1),
			},
			[]scraper.RemoteFile{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			sort.Slice(tc.Locals, func(i, j int) bool {
				return tc.Locals[i].SortName < tc.Locals[j].SortName
			})
			sort.Slice(tc.Remotes, func(i, j int) bool {
				return tc.Remotes[i].SortName < tc.Remotes[j].SortName
			})

			extra, missing, changed := diffSortedFiles(tc.Locals, tc.Remotes)

			if len(extra) != len(tc.ExpectedExtra) {
				t.Fatalf(
					"expected %d extra, but got %d\n\texpected: %v\n\tactual: %v",
					len(tc.ExpectedExtra), len(extra), tc.ExpectedExtra, extra,
				)
			}
			for i := range extra {
				if extra[i].Name != tc.ExpectedExtra[i].Name {
					t.Errorf("extra %d: name mismatch: '%s', but expected '%s'", i, extra[i].Name, tc.ExpectedExtra[i].Name)
				}
				if extra[i].Timestamp != tc.ExpectedExtra[i].Timestamp {
					t.Errorf("extra %d: name mismatch: '%s', but expected '%s'", i, extra[i].Name, tc.ExpectedExtra[i].Name)
				}
				if extra[i].Size != tc.ExpectedExtra[i].Size {
					t.Errorf("extra %d: name mismatch: '%s', but expected '%s'", i, extra[i].Name, tc.ExpectedExtra[i].Name)
				}
			}

			if len(missing) != len(tc.ExpectedMissing) {
				t.Fatalf(
					"expected %d missing, but got %d\n\texpected: %v\n\tactual: %v",
					len(tc.ExpectedMissing), len(missing), tc.ExpectedMissing, missing,
				)
			}
			for i := range missing {
				if missing[i].Name != tc.ExpectedMissing[i].Name {
					t.Errorf("missing %d: name mismatch: '%s', but expected '%s'", i, missing[i].Name, tc.ExpectedMissing[i].Name)
				}
				if missing[i].Timestamp != tc.ExpectedMissing[i].Timestamp {
					t.Errorf("missing %d: timestamp mismatch: '%s', but expected '%s'", i, missing[i].Timestamp, tc.ExpectedMissing[i].Timestamp)
				}
				if missing[i].Size != tc.ExpectedMissing[i].Size {
					t.Errorf("changed %d: size mismatch: '%d', but expected '%d'", i, missing[i].Size, tc.ExpectedMissing[i].Size)
				}
			}

			if len(changed) != len(tc.ExpectedChanged) {
				t.Fatalf(
					"expected %d changed, but got %d\n\texpected: %v\n\tactual: %v",
					len(tc.ExpectedChanged), len(changed), tc.ExpectedChanged, changed,
				)
			}
			for i := range changed {
				if changed[i].Name != tc.ExpectedChanged[i].Name {
					t.Errorf("changed %d: name mismatch: '%s', but expected '%s'", i, changed[i].Name, tc.ExpectedChanged[i].Name)
				}
				if changed[i].Timestamp != tc.ExpectedChanged[i].Timestamp {
					t.Errorf("changed %d: timestamp mismatch: '%s', but expected '%s'", i, changed[i].Timestamp, tc.ExpectedChanged[i].Timestamp)
				}
				if changed[i].Size != tc.ExpectedChanged[i].Size {
					t.Errorf("changed %d: size mismatch: '%d', but expected '%d'", i, changed[i].Size, tc.ExpectedChanged[i].Size)
				}
			}
		})
	}
}

func localFile(t *testing.T, name, stamp string, size int64) LocalFile {
	t.Helper()
	var ts time.Time
	if len(stamp) > 0 {
		var err error
		ts, err = time.Parse("2006-01-02 15:04", stamp)
		if err != nil {
			t.Fatalf("error parsing time: %v", err)
		}
	}
	return LocalFile{
		Name:      name,
		SortName:  strings.ToLower(name),
		Timestamp: ts,
		Size:      size,
	}
}

func remoteFile(t *testing.T, name, stamp string, size int64) scraper.RemoteFile {
	t.Helper()
	var ts time.Time
	if len(stamp) > 0 {
		var err error
		ts, err = time.Parse("2006-01-02 15:04", stamp)
		if err != nil {
			t.Fatalf("error parsing time: %v", err)
		}
	}
	return scraper.RemoteFile{
		Name:      name,
		SortName:  strings.ToLower(name),
		URL:       name,
		Timestamp: ts,
		Size:      size,
	}
}
