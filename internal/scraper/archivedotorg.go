package scraper

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ArchiveDotOrg struct {
	BaseURL   string
	UserAgent string
}

func init() {
	Register("archive.org", func(name string, opts ...Option) (Scraper, error) {
		var baseURL string
		for _, o := range opts {
			switch ot := o.(type) {
			case optBaseURL:
				baseURL = ot.v
			}
		}
		if len(baseURL) == 0 {
			return nil, fmt.Errorf("missing required option: BaseURL")
		}
		return &ArchiveDotOrg{
			BaseURL: baseURL,
		}, nil
	})
}

// (2023-10-07) archive.org seems to have two different responses, sometimes depending on
// if the URL ends in a '/'.
//
// For example, compare the source resulting from these two requests:
//
//	https://archive.org/download/images/tv
//	https://archive.org/download/images/tv/
//
// As of the date of this comment, the former is a much simpler http response, with the actual
// file size in bytes, while the latter includes a lot of extra html, spreads each file across
// multiple lines, and only includes a humanized file size.
//
// This scraper attempts to support both, and uses adoSourceType to note which one we think
// we've encountered.

type adoSourceType int

const (
	adostSimple adoSourceType = iota
	adostFull
)

func (n ArchiveDotOrg) ScrapeRemotes() ([]RemoteFile, error) {
	remotes := make([]RemoteFile, 0, 256)

	req, err := http.NewRequest("GET", n.BaseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to make new GET request: %w", err)
	}
	if len(n.UserAgent) > 0 {
		req.Header.Set("User-Agent", n.UserAgent)
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected request status %d", resp.StatusCode)
	}

	return n.ScrapeFromReader(resp.Body, remotes)
}

func (n ArchiveDotOrg) ScrapeFromReader(r io.Reader, remotes []RemoteFile) ([]RemoteFile, error) {
	scanner := bufio.NewScanner(r)
	adoType, err := n.readType(scanner)
	if err != nil {
		return nil, fmt.Errorf("error parsing response body: %w", err)
	}

	switch adoType {
	case adostSimple:
		remotes, err = n.parseSimple(scanner, remotes)
		if err != nil {
			return nil, fmt.Errorf("error parsing as 'simple': %w", err)
		}
	case adostFull:
		remotes, err = n.parseFull(scanner, remotes)
		if err != nil {
			return nil, fmt.Errorf("error parsing as 'full': %w", err)
		}
	default:
		return nil, fmt.Errorf("unrecognized adoType %d", adoType)
	}

	return remotes, nil
}

func (n ArchiveDotOrg) readType(s *bufio.Scanner) (adoSourceType, error) {
	var line string
	if s.Scan() {
		line = strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "<!DOCTYPE html>") {
			return adostFull, nil
		}
		if strings.HasPrefix(line, "<html>") {
			return adostSimple, nil
		}
	}
	if err := s.Err(); err != nil {
		return 0, fmt.Errorf("failed to scan first line: %w", err)
	}
	return 0, fmt.Errorf("unrecognized first line '%s'", line)
}

var adoSimpleFileLineRE = regexp.MustCompile(`^<a href="([^"]+)">(.[^<]+)<\/a>\s*([0-9]+\-[a-zA-Z]+\-[0-9]+ [0-9]+:[0-9]+)\s+([0-9]+)$`)

func (n ArchiveDotOrg) parseSimple(scanner *bufio.Scanner, remotes []RemoteFile) ([]RemoteFile, error) {
	for scanner.Scan() {
		line := scanner.Text()
		matches := adoSimpleFileLineRE.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		urlStr := matches[1]
		// fileName := matches[2] // CANNOT TRUST THIS: it may be cut short and end in a '..>'
		timeStr := matches[3]
		sizeStr := matches[4]

		fileURL, err := url.Parse(urlStr)
		if err != nil {
			return remotes, fmt.Errorf("failed to parse url '%s': %w", urlStr, err)
		}

		if !fileURL.IsAbs() {
			fileURL, err = url.Parse(n.BaseURL)
			if err != nil {
				return remotes, fmt.Errorf("failed to parse base url '%s': %w", n.BaseURL, err)
			}
			fileURL = fileURL.JoinPath(urlStr)
		}

		fileName := path.Base(fileURL.Path)

		lastModified, err := time.Parse("02-Jan-2006 15:04", timeStr)
		if err != nil {
			return remotes, fmt.Errorf("failed to parse time '%s': %w", timeStr, err)
		}

		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			return remotes, fmt.Errorf("failed to parse size '%s': %w", sizeStr, err)
		}

		remotes = append(remotes, RemoteFile{
			Name:      fileName,
			SortName:  strings.ToLower(fileName),
			URL:       fileURL.String(),
			Timestamp: lastModified,
			Size:      size,
		})
	}
	if err := scanner.Err(); err != nil {
		return remotes, fmt.Errorf("failed to scan response body: %w", err)
	}

	return remotes, nil
}

var (
	adoFullFileNameHeader = regexp.MustCompile(`^\s+<td><a href="[^"]+"><span class="iconochive-Uplevel" title="Parent Directory" aria-hidden="true"><\/span> Go to parent directory<\/a><\/td>$`)
	adoFullFileNameLineRE = regexp.MustCompile(`^\s+<td><a href="([^"]+)">([^<]+)<\/a>.*<\/td>$`)
	adoFullLastModifiedRE = regexp.MustCompile(`^\s+<td>([0-9]+\-[a-zA-Z]+\-[0-9]+ [0-9]+:[0-9]+)<\/td>$`)
)

func (n ArchiveDotOrg) parseFull(scanner *bufio.Scanner, remotes []RemoteFile) ([]RemoteFile, error) {
	// scan down to the top of the file list
	foundFileList := false
	for scanner.Scan() {
		line := scanner.Text()
		if adoFullFileNameHeader.MatchString(line) {
			foundFileList = true
			break
		}
	}
	if !foundFileList {
		return remotes, fmt.Errorf("failed to find file list")
	}

	// start looking for files
	for scanner.Scan() {
		line := scanner.Text()
		matches := adoFullFileNameLineRE.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		urlStr := matches[1]
		// fileName := matches[2] // after problems in the Simple case above, I don't trust this to be the full name

		fileURL, err := url.Parse(urlStr)
		if err != nil {
			return remotes, fmt.Errorf("failed to parse url '%s': %w", urlStr, err)
		}
		if !fileURL.IsAbs() {
			fileURL, err = url.Parse(n.BaseURL)
			if err != nil {
				return remotes, fmt.Errorf("failed to parse base url '%s': %w", n.BaseURL, err)
			}
			fileURL = fileURL.JoinPath(urlStr)
		}
		fileName := path.Base(fileURL.Path)

		// last modified time should be on the next line
		matches = nil
		if scanner.Scan() {
			line = scanner.Text()
			matches = adoFullLastModifiedRE.FindStringSubmatch(line)
		}
		if matches == nil {
			return remotes, fmt.Errorf("failed to find last modified time for '%s'", fileName)
		}

		timeStr := matches[1]
		lastModified, err := time.Parse("02-Jan-2006 15:04", timeStr)
		if err != nil {
			return remotes, fmt.Errorf("failed to parse time '%s': %w", timeStr, err)
		}

		remotes = append(remotes, RemoteFile{
			Name:      fileName,
			SortName:  strings.ToLower(fileName),
			URL:       fileURL.String(),
			Timestamp: lastModified,
			Size:      -1,
		})
	}
	if err := scanner.Err(); err != nil {
		return remotes, fmt.Errorf("error while scanning: %w", err)
	}

	return remotes, nil
}
