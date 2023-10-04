package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danbrakeley/frog"
	"github.com/danbrakeley/needl/internal/buildvar"
	"github.com/danbrakeley/needl/internal/config"
	"github.com/danbrakeley/needl/internal/scraper"
)

const (
	defaultConfigPath   = "needl.toml"
	defaultScrapersPath = "scrapers.toml"
	defaultThreadCount  = 4
)

func PrintUsage() {
	version := "<local build>"
	if len(buildvar.Version) > 0 {
		version = buildvar.Version
	}
	buildTime := "<not set>"
	if len(buildvar.BuildTime) > 0 {
		buildTime = buildvar.BuildTime
	}
	url := "https://github.com/danbrakeley/needl"
	if len(buildvar.ReleaseURL) > 0 {
		url = buildvar.ReleaseURL
	}

	fmt.Fprintf(flag.CommandLine.Output(),
		strings.Join([]string{
			"",
			"needl %s, created %s",
			"%s",
			"",
			"Usage:",
			"\tneedl [options] <scraper_name> <download_path>",
			"\tneedl --version",
			"\tneedl --help",
			"Options:",
			"\t-c, --config PATH     Config TOML file (default: '%s')",
			"\t    --scrapers PATH   Scrapers TOML file (default: '%s')",
			"\t-t, --threads NUM     Max number of concurrent downloads (default: '%d')",
			"\t-v, --verbose         Extra output (for debugging)",
			"\t    --version         Print just the version number (to stdout)",
			"\t-h, --help            Print this message (to stderr)",
			"",
		}, "\n"), version, buildTime, url, defaultConfigPath, defaultScrapersPath, defaultThreadCount,
	)
}

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

func mainExit() int {
	start := time.Now()
	flag.Usage = PrintUsage

	var configPath string
	var scrapersPath string
	var threadCount int
	var verbose bool
	var showVersion bool
	var showHelp bool
	flag.StringVar(&configPath, "config", defaultConfigPath, "path to optional config file")
	flag.StringVar(&configPath, "c", defaultConfigPath, "path to optional config file")
	flag.StringVar(&scrapersPath, "scrapers", defaultScrapersPath, "path to scrapers file")
	flag.IntVar(&threadCount, "threads", 0, "number of simultaneous downloads")
	flag.IntVar(&threadCount, "t", 0, "number of simultaneous downloads")
	flag.BoolVar(&verbose, "v", false, "extra logging for debugging")
	flag.BoolVar(&verbose, "verbose", false, "extra logging for debugging")
	flag.BoolVar(&showVersion, "version", false, "show version info")
	flag.BoolVar(&showHelp, "h", false, "show this help message")
	flag.BoolVar(&showHelp, "help", false, "show this help message")
	flag.Parse()

	if showVersion {
		if len(buildvar.Version) == 0 {
			fmt.Printf("unknown\n")
			return 1
		}
		fmt.Printf("%s\n", strings.TrimPrefix(buildvar.Version, "v"))
		return 0
	}

	if showHelp {
		flag.Usage()
		return 0
	}

	if len(flag.Args()) > 2 {
		fmt.Printf("unrecognized arguments: %v\n", strings.Join(flag.Args(), " "))
		flag.Usage()
		return 1
	}

	log := frog.New(frog.Auto, frog.POFieldIndent(26))
	if verbose {
		log.SetMinLevel(frog.Verbose)
	}
	defer func() {
		dur := time.Now().Sub(start)
		log.Info("Done", frog.Dur("time", dur))
		log.Close()
	}()

	// parse arguments
	var scraperName string
	var dstPath string
	scraperName = flag.Arg(0)
	dstPath = flag.Arg(1)

	log.Info("Loading config...", frog.Path(configPath))
	cfg, err := config.Load(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Error("loading config", frog.PathAbs(configPath), frog.Err(err))
		return 5
	}

	// override config with command-line flags
	if len(scraperName) > 0 {
		cfg.Scraper = scraperName
	}
	if len(dstPath) > 0 {
		cfg.LocalPath = dstPath
	}
	if threadCount > 0 {
		cfg.Threads = threadCount
	} else if cfg.Threads == 0 {
		cfg.Threads = defaultThreadCount
	}
	if verbose {
		cfg.Verbose = true
	}
	// now that the config is loaded, ensure the log level is set properly
	if cfg.Verbose {
		log.SetMinLevel(frog.Verbose)
	} else {
		log.SetMinLevel(frog.Info)
	}

	log.Info("Loading scrapers...", frog.Path(scrapersPath))
	scrapers, err := config.LoadScrapers(scrapersPath)
	if err != nil {
		log.Error("loading scrapers", frog.PathAbs(scrapersPath), frog.Err(err))
		return 6
	}

	scfg, ok := scrapers[cfg.Scraper]
	if !ok {
		log.Error("scraper not found", frog.String("name", cfg.Scraper), frog.PathAbs(scrapersPath))
		log.Close()
		flag.CommandLine.SetOutput(os.Stderr)
		flag.Usage()
		if len(scrapers) == 0 {
			fmt.Fprintf(os.Stderr, "\nno scrapers found in %s\n", filepath.ToSlash(scrapersPath))
		} else {
			fmt.Fprintf(os.Stderr, "\navailable scrapers (from %s):\n", filepath.ToSlash(scrapersPath))
			for k := range scrapers {
				fmt.Fprintf(os.Stderr, "\t%s\n", k)
			}
		}
		return 7
	}

	// ensure local path exists
	if err := os.MkdirAll(cfg.LocalPath, 0o755); err != nil {
		log.Error("creating local path", frog.PathAbs(cfg.LocalPath), frog.Err(err))
	}

	// list local and remote files
	locals, remotes, errno := listFiles(log, cfg, scfg)
	if errno > 0 {
		return errno
	}

	// diff local vs remote
	// both lists are sorted, so the diff is at worst O(n+m)
	extra := make([]LocalFile, 0, len(locals))
	missing := make([]scraper.RemoteFile, 0, len(remotes))
	changed := make([]scraper.RemoteFile, 0, len(remotes))
	i, j := 0, 0
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

	// call out files that are local-only
	for _, v := range extra {
		log.Info("Local file not in remote", frog.String("name", v.Name))
	}

	var wg sync.WaitGroup
	ch := make(chan scraper.RemoteFile)
	// spawn workers
	wg.Add(cfg.Threads)
	for i := 0; i < cfg.Threads; i++ {
		go func() {
			for r := range ch {
				log.Info("Start download",
					frog.String("name", r.Name), frog.Int64("size", r.Size),
					frog.Time("time", r.Timestamp), frog.String("url", r.URL),
				)
				path := filepath.Join(cfg.LocalPath, r.Name)
				res, err := DownloadToFile(log, r.URL, path,
					DownloadOptions{ExpectedSize: r.Size, ExpectedLastModified: r.Timestamp},
				)
				if err != nil {
					log.Error("unrecoverable error",
						frog.String("name", r.Name), frog.Int64("size", res.ActualSize),
						frog.Time("time", res.LastModified), frog.String("url", r.URL),
						frog.PathAbs(path), frog.Err(err),
					)
					continue
				}
				log.Info("File written", frog.String("name", r.Name),
					frog.Time("time", r.Timestamp), frog.Int64("size", r.Size),
					frog.Path(path),
				)
			}
			wg.Done()
		}()
	}

	if cfg.Verbose {
		for _, v := range changed {
			log.Verbose("queuing changed file", frog.String("name", v.Name))
		}
		for _, v := range missing {
			log.Verbose("queuing missing file", frog.String("name", v.Name))
		}
	}

	// feed work to the workers
	for _, v := range changed {
		ch <- v
	}
	for _, v := range missing {
		ch <- v
	}

	// let idle workers know they can stop
	close(ch)
	// wait for all workers to complete and shutdown
	wg.Wait()

	return 0
}

// listFiles concurrently lists both the local and remote files
func listFiles(log frog.Logger, cfg config.Config, scfg config.Scraper) ([]LocalFile, []scraper.RemoteFile, int) {
	var locals []LocalFile
	var errLocal error
	var remotes []scraper.RemoteFile
	var errRemote error

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		log.Info("Listing local files...", frog.Path(cfg.LocalPath))
		locals, errLocal = getSortedLocals(cfg.LocalPath)
	}()

	go func() {
		defer wg.Done()
		log.Info("Listing remote files...", frog.String("url", scfg.URL))
		remotes, errRemote = getSortedRemotes(scfg)
	}()

	wg.Wait()

	if errLocal != nil {
		log.Error("list local files", frog.Err(errLocal), frog.PathAbs(cfg.LocalPath))
		return nil, nil, 20
	}

	if errRemote != nil {
		log.Error("list remote files", frog.Err(errRemote), frog.String("url", scfg.URL))
		return nil, nil, 30
	}

	return locals, remotes, 0
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

func getSortedRemotes(scfg config.Scraper) ([]scraper.RemoteFile, error) {
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
