package scraper

import (
	"bufio"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ArchiveDotOrg struct {
	BaseURL   string
	UserAgent string
}

var archiveDotOrgFileLineRE = regexp.MustCompile(`^<a href="([^"]+)">.*<\/a>\s+([0-9]+\-[a-zA-Z]+\-[0-9]+ [0-9]+:[0-9]+)\s+([0-9]+)$`)

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

	// match each line to regular expression

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		matches := archiveDotOrgFileLineRE.FindStringSubmatch(line)
		if matches != nil {
			nameEscaped := matches[1]
			stampStr := matches[2]
			sizeStr := matches[3]

			name, err := url.PathUnescape(nameEscaped)
			if err != nil {
				return nil, fmt.Errorf("failed to unescape '%s': %w", nameEscaped, err)
			}

			stamp, err := time.Parse("02-Jan-2006 15:04", stampStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse time '%s': %w", stampStr, err)
			}

			size, err := strconv.ParseInt(sizeStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse size '%s': %w", sizeStr, err)
			}

			urlPath, err := url.JoinPath(n.BaseURL, name)
			if err != nil {
				return nil, fmt.Errorf("failed to join '%s' with '%s': %w", n.BaseURL, name, err)
			}

			remotes = append(remotes, RemoteFile{
				Name:      name,
				SortName:  strings.ToLower(name),
				URL:       urlPath,
				Timestamp: stamp,
				Size:      size,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan response body: %w", err)
	}

	return remotes, nil
}
