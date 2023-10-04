# needl

`needl` is a scraper and downloader.

It was originally written to keep folders on archive.org in sync with local files.

## Usage

```text
Usage:
        needl [options] <scraper_name> <download_path>
        needl --version
        needl --help
Options:
        -c, --config PATH     Config TOML file (default: 'needl.toml')
            --scrapers PATH   Scrapers TOML file (default: 'scrapers.toml')
        -t, --threads NUM     Max number of concurrent downloads (default: '4')
        -v, --verbose         Extra output (for debugging)
            --version         Print just the version number (to stdout)
        -h, --help            Print this message (to stderr)
```

Note that you must have a `scrapers.toml` file in the following format:

```toml
[tvimages]
type = "archive.org"
url = "https://archive.org/download/images/tv"
```

Optionally, you can also specify a `needl.toml`, instead of passing arguments on the command line:

```toml
path = "./downloads"
scraper = "tvimages"
threads = 8
verbose = true
```
