package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danbrakeley/frog"
	"github.com/danbrakeley/needl/internal/config"
	"github.com/danbrakeley/needl/internal/scraper"
)

func main() {
	status := mainExit()
	if status != 0 {
		// From os/proc.go: "For portability, the status code should be in the range [0, 125]."
		if status < 0 || status > 125 {
			status = 125
		}
		os.Exit(status)
	}
}

type LocalFile struct {
	Name      string
	SortName  string
	Timestamp time.Time
	Size      int64
}

var log frog.RootLogger

func mainExit() int {
	var help bool
	flag.BoolVar(&help, "help", false, "show this help message")
	flag.BoolVar(&help, "h", false, "show this help message")

	const defaultConfigPath = "config.toml"
	var configPath string
	flag.StringVar(&configPath, "config", defaultConfigPath, "path to config file")
	flag.StringVar(&configPath, "c", defaultConfigPath, "path to config file")

	const defaultThreadCount = 4
	var threadCount int
	flag.IntVar(&threadCount, "threads", defaultThreadCount, "number of simultaneous downloads")
	flag.IntVar(&threadCount, "t", defaultThreadCount, "number of simultaneous downloads")

	var usageStr strings.Builder
	usageStr.Grow(1024)
	usageStr.WriteString("usage: ")
	usageStr.WriteString(filepath.Base(os.Args[0]))
	usageStr.WriteString(" [options] <scraper-type>\n\noptions:\n")
	usageStr.WriteString("  --config, -c <config-file>   path to config file (defauilt: ")
	usageStr.WriteString(defaultConfigPath)
	usageStr.WriteString(")\n")
	usageStr.WriteString("  --threads, -t <threads>      number of simultaneous downloads (defauilt: ")
	usageStr.WriteString(strconv.Itoa(defaultThreadCount))
	usageStr.WriteString(")\n")
	usageStr.WriteString("  --help, -h                   show this help message\n")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "%s", usageStr.String())
	}

	flag.Parse()
	if flag.NArg() > 1 {
		flag.CommandLine.SetOutput(os.Stderr)
		flag.Usage()
		return 1
	}
	scraperName := flag.Arg(0)

	log = frog.New(frog.Auto, frog.POFieldIndent(25))
	defer log.Close()
	log.SetMinLevel(frog.Verbose)

	log.Info("Loading config...", frog.String("path", configPath))
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		log.Error("unable to load config", frog.Err(err))
		log.Close()
		flag.CommandLine.SetOutput(os.Stderr)
		fmt.Fprintf(os.Stderr, "\n")
		flag.Usage()
		return 10
	}

	scfg, ok := cfg.Scrapers[scraperName]
	if !ok {
		log.Error("scraper not found", frog.String("name", scraperName))
		log.Close()
		flag.CommandLine.SetOutput(os.Stderr)
		fmt.Fprintf(os.Stderr, "\n")
		flag.Usage()
		if len(cfg.Scrapers) == 0 {
			fmt.Fprintf(os.Stderr, "\nno scrapers found in %s\n", configPath)
		} else {
			fmt.Fprintf(os.Stderr, "\navailable scrapers (from %s):\n", configPath)
			for k := range cfg.Scrapers {
				fmt.Fprintf(os.Stderr, "  %s\n", k)
			}
		}
		return 11
	}

	log.Info("Listing local files...")
	locals, err := getSortedLocals(".")
	if err != nil {
		log.Error("unable to list local files", frog.Err(err))
		return 20
	}

	log.Info("Listing remote files...")
	remotes, err := getSortedRemotes(scfg)
	if err != nil {
		log.Error("unable to list remote files", frog.Err(err))
		return 30
	}

	i, j := 0, 0

	extra := make([]LocalFile, 0, len(locals))
	missing := make([]scraper.RemoteFile, 0, len(remotes))
	changed := make([]scraper.RemoteFile, 0, len(remotes))

	for i < len(locals) && j < len(remotes) {
		local := locals[i]
		remote := remotes[j]

		if local.SortName < remote.SortName {
			extra = append(extra, local)
			i++
			continue
		}

		if local.SortName > remote.SortName {
			missing = append(missing, remote)
			j++
			continue
		}

		if !local.Timestamp.Equal(remote.Timestamp) || local.Size != remote.Size {
			changed = append(changed, remote)
		}

		i++
		j++
	}

	for i < len(locals) {
		extra = append(extra, locals[i])
		i++
	}

	for j < len(remotes) {
		missing = append(missing, remotes[j])
		j++
	}

	// warn the user about files that are local-only
	for _, v := range extra {
		log.Info("Local file not in remote", frog.String("name", v.Name))
	}

	var wg sync.WaitGroup
	ch := make(chan scraper.RemoteFile)
	// spawn workers
	wg.Add(threadCount)
	for i := 0; i < threadCount; i++ {
		go func() {
			for r := range ch {
				log.Info(
					"Downloading",
					frog.String("name", r.Name),
					frog.String("url", r.URL),
					frog.Time("time", r.Timestamp),
					frog.Int64("size", r.Size),
				)
				res, err := DownloadToFile(log, r.URL, r.Name, DownloadOptions{
					ExpectedSize:         r.Size,
					ExpectedLastModified: r.Timestamp,
				})
				if err != nil {
					log.Error(
						"unrecoverable error",
						frog.String("name", r.Name),
						frog.String("url", r.URL),
						frog.Time("time", res.LastModified),
						frog.Int64("size", res.ActualSize),
						frog.Err(err),
					)
					continue
				}
				log.Info("Success", frog.Time("time", r.Timestamp), frog.String("name", r.Name))
			}
			wg.Done()
		}()
	}

	for _, v := range changed {
		log.Info("Queuing changed file", frog.String("name", v.Name))
	}
	for _, v := range missing {
		log.Info("Queuing missing file", frog.String("name", v.Name))
	}
	for _, v := range changed {
		ch <- v
	}
	for _, v := range missing {
		ch <- v
	}

	close(ch)
	wg.Wait()

	log.Info("Done")

	return 0
}

func getSortedLocals(path string) ([]LocalFile, error) {
	locals := make([]LocalFile, 0, 256)

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		i, err := e.Info()
		if err != nil {
			return nil, err
		}
		locals = append(locals, LocalFile{
			Name:      e.Name(),
			SortName:  strings.ToLower(e.Name()),
			Timestamp: i.ModTime().UTC(),
			Size:      i.Size(),
		})
	}

	sort.Slice(locals, func(i, j int) bool {
		return locals[i].SortName < locals[j].SortName
	})

	return locals, nil
}

func getSortedRemotes(scfg config.ScraperConfig) ([]scraper.RemoteFile, error) {
	var scrpr scraper.Scraper
	switch scfg.Type {
	case "archive.org":
		scrpr = scraper.ArchiveDotOrg{
			BaseURL: scfg.URL,
		}
	default:
		return nil, fmt.Errorf("unknown scraper type '%s'", scfg.Type)
	}

	remotes, err := scrpr.ScrapeRemotes()
	if err != nil {
		return nil, fmt.Errorf("error scraping for files: %w", err)
	}

	sort.Slice(remotes, func(i, j int) bool {
		return remotes[i].SortName < remotes[j].SortName
	})

	return remotes, nil
}
