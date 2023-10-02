package main

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/danbrakeley/frog"
	"github.com/dustin/go-humanize"
	"github.com/natefinch/atomic"
)

type DownloadOptions struct {
	// ExpectedSize is the size in bytes that must be downloaded for this
	// download to be succeed, or zero if the size is not known up front.
	// If ExpectedSize is non-zero, then we verify any Content-Length header
	// matches this value.
	// If ExpectedSize is zero, but the server provided a Content-Length
	// header, the final downloaded size is verified against that value.
	ExpectedSize int64

	// ExpectedLastModified is used to validate any Last-Modified header
	// received from the server.
	// If zero, or there is no Last-Modified header, then it is ignored.
	ExpectedLastModified time.Time

	// MaxRetry is the maximum number of times to retry after an error.
	// If zero, then will retry forever.
	MaxRetry uint
}

type DownloadResults struct {
	ExpectedSize int64
	ActualSize   int64
	LastModified time.Time
	Retries      uint
}

func DownloadToFile(
	log frog.Logger,
	remoteURL string,
	localPath string,
	opts DownloadOptions,
) (DownloadResults, error) {
	if log == nil {
		log = &frog.NullLogger{}
	} else {
		log = frog.AddAnchor(log)
		defer frog.RemoveAnchor(log)
	}

	log.Transient("starting download",
		frog.Int64("size", opts.ExpectedSize),
		frog.Uint("max_retry", opts.MaxRetry),
		frog.String("url", remoteURL),
	)

	res := DownloadResults{
		ExpectedSize: opts.ExpectedSize,
		ActualSize:   0,
		LastModified: opts.ExpectedLastModified,
		Retries:      0,
	}

	tmpPath := localPath + ".tmp"
	log.Verbose("creating file", frog.String("path", tmpPath))

	f, err := os.Create(tmpPath)
	if err != nil {
		return res, fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	dc := downloadContext{
		remoteURL: remoteURL,
		opts:      opts,
		bytesRead: 0,
		curRetry:  0,
	}

	err = dc.downloadImpl(log, f, false)
	// this is useful to have up to date even if there's an error...
	res.ExpectedSize = dc.opts.ExpectedSize
	res.ActualSize = dc.bytesRead
	res.LastModified = dc.opts.ExpectedLastModified
	res.Retries = dc.curRetry
	// ... and then handle the error
	if err != nil {
		return res, err
	}

	if err := f.Close(); err != nil {
		return res, fmt.Errorf("close file: %w", err)
	}

	log.Transient("moving", frog.String("dst", localPath), frog.String("src", tmpPath))
	if err := atomic.ReplaceFile(tmpPath, localPath); err != nil {
		return res, fmt.Errorf("move: %w", err)
	}

	log.Transient("setting file time", frog.Time("time", res.LastModified), frog.String("name", localPath))
	if err := modifyFileTime(localPath, res.LastModified); err != nil {
		return res, fmt.Errorf("set time failed: %w", err)
	}

	return res, nil
}

type downloadContext struct {
	remoteURL string
	opts      DownloadOptions
	bytesRead int64
	curRetry  uint
}

// downloadImpl does the downloading, including retrying and resuming
func (dc *downloadContext) downloadImpl(
	log frog.Logger,
	f *os.File,
	canResume bool,
) error {
	if dc.opts.MaxRetry > 0 && dc.curRetry >= dc.opts.MaxRetry {
		return fmt.Errorf("max retries (%d) exceeded", dc.opts.MaxRetry)
	}

	req, err := http.NewRequest("GET", dc.remoteURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if canResume && dc.bytesRead > 0 {
		log.Verbose("resume download",
			frog.Int64("start", dc.bytesRead),
			frog.Int64("total", dc.opts.ExpectedSize),
			frog.Uint("cur_retry", dc.curRetry),
			frog.Uint("max_retry", dc.opts.MaxRetry),
			frog.String("url", dc.remoteURL),
		)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", dc.bytesRead))
	} else {
		log.Verbose("start download",
			frog.Int64("total", dc.opts.ExpectedSize),
			frog.Uint("cur_retry", dc.curRetry),
			frog.Uint("max_retry", dc.opts.MaxRetry),
			frog.String("url", dc.remoteURL),
		)
	}

	fnRetryOrErr := func(err error) error {
		dc.curRetry += 1
		if dc.opts.MaxRetry > 0 && dc.curRetry >= dc.opts.MaxRetry {
			return err
		}

		// backoff
		d := backoff(dc.curRetry)
		log.Verbose("pausing before retry",
			frog.Dur("backoff", d),
			frog.Err(err),
			frog.Int64("bytes_read", dc.bytesRead),
			frog.Int64("size", dc.opts.ExpectedSize),
			frog.Uint("cur_retry", dc.curRetry),
			frog.Uint("max_retry", dc.opts.MaxRetry),
			frog.String("url", dc.remoteURL),
		)
		time.Sleep(d)

		// and retry, as appropriate
		return dc.downloadImpl(log, f, canResume)
	}

	// begin request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fnRetryOrErr(fmt.Errorf("do request: %w", err))
	}
	defer resp.Body.Close()

	// parse headers and validate assumptions

	if !canResume {
		canResume = parseCanResume(resp.Header)
	}

	if !canResume && dc.bytesRead > 0 {
		// we expected to resume, but the server doesn't support it, so drop the previously downloaded data
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek to start: %w", err)
		}
		if err := f.Truncate(0); err != nil {
			return fmt.Errorf("truncate: %w", err)
		}
		dc.bytesRead = 0
		log.Verbose("truncating file",
			frog.Int64("bytes_read", dc.bytesRead),
			frog.Int64("size", dc.opts.ExpectedSize),
			frog.Uint("cur_retry", dc.curRetry),
			frog.Uint("max_retry", dc.opts.MaxRetry),
			frog.String("url", dc.remoteURL),
		)
	}

	cl := parseContentLength(resp.Header)
	if cl > 0 {
		if canResume {
			if dc.opts.ExpectedSize > 0 {
				expectedCl := dc.opts.ExpectedSize - dc.bytesRead
				if cl != expectedCl {
					return fmt.Errorf("expected remaining Content-Length to be %d, but is %d", expectedCl, cl)
				}
			} else {
				dc.opts.ExpectedSize = dc.bytesRead + cl
			}
		} else {
			if dc.opts.ExpectedSize > 0 && cl != dc.opts.ExpectedSize {
				return fmt.Errorf("expected Content-Length to be %d, but is %d", dc.opts.ExpectedSize, cl)
			}
			dc.opts.ExpectedSize = cl
		}
	}

	mt := parseLastModifiedMinute(resp.Header)
	if !mt.IsZero() {
		if dc.opts.ExpectedLastModified.IsZero() {
			dc.opts.ExpectedLastModified = mt
		} else if !mt.Equal(dc.opts.ExpectedLastModified) {
			return fmt.Errorf("expected Last-Modified to be %v, but is %v", dc.opts.ExpectedLastModified, mt)
		}
	}

	// download file contents
	pw := newProgressWriter(log, dc.remoteURL, dc.opts.ExpectedSize-dc.bytesRead)
	n, err := io.Copy(io.MultiWriter(f, pw), resp.Body)
	dc.bytesRead += n
	if err != nil {
		// ensure previous body is closed (TODO: is this necessary?)
		// purposely ignoring the error here, because we're already in an error state
		_ = resp.Body.Close()
		return fnRetryOrErr(fmt.Errorf("download: %w", err))
	}

	// properly close the body, dealing with any errors
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close response body: %w", err)
	}

	// validate we downloaded what we expected to download
	if dc.opts.ExpectedSize > 0 && dc.bytesRead != dc.opts.ExpectedSize {
		return fmt.Errorf("expected final size to be %d, but is %d", dc.opts.ExpectedSize, dc.bytesRead)
	}

	return nil
}

func newProgressWriter(log frog.Logger, URL string, total int64) io.Writer {
	return &progressWriter{
		log:       log,
		remoteURL: URL,
		total:     total,
		totalStr:  humanize.Bytes(uint64(total)),
	}
}

type progressWriter struct {
	log        frog.Logger
	remoteURL  string
	total      int64 // for the percent math
	progress   int64
	totalStr   string // humanized copy of Total
	lastUpdate time.Time
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	const timeBetweenUpdates = time.Millisecond * 500
	n := len(p)
	pw.progress += int64(n)
	if pw.lastUpdate.IsZero() || time.Since(pw.lastUpdate) > timeBetweenUpdates {
		pw.log.Transient(
			"download progress",
			frog.String("total", pw.totalStr),
			// frog.Int64("progress", pw.Progress),
			frog.String("percent", fmt.Sprintf("%.2f%%", float64(pw.progress)/float64(pw.total)*100)),
			frog.String("url", pw.remoteURL),
		)
		pw.lastUpdate = time.Now()
	}
	return n, nil
}

func backoff(curRetry uint) time.Duration {
	e := uint64(curRetry)
	if e > 10 {
		e = 10 // 2^10 = 1024, 1024 * 500ms = 512s = 8m32s
	}
	ms := intPow(2, e) * 500
	jitter := uint64(rand.Int63n(int64(ms) / 10))
	return time.Duration(ms+jitter) * time.Millisecond
}

// from https://stackoverflow.com/questions/64108933/how-to-use-math-pow-with-integers-in-golang
func intPow[N int | int32 | int64 | uint | uint32 | uint64](base, exp N) N {
	var result N = 1
	for {
		if exp&1 == 1 {
			result *= base
		}
		exp >>= 1
		if exp == 0 {
			break
		}
		base *= base
	}
	return result
}

// Header parsing helpers

// parseCanResume returns true if the Accept-Ranges header is present and set to "bytes"
func parseCanResume(h http.Header) bool {
	return h.Get("Accept-Ranges") == "bytes"
}

// parseContentLength returns -1 if the header is not present or cannot be parsed
func parseContentLength(h http.Header) int64 {
	lenRaw := h.Get("Content-Length")
	if len(lenRaw) == 0 {
		return -1
	}
	n, err := strconv.ParseInt(lenRaw, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// parseLastModifiedMinute returns zero if the header is not present or cannot be parsed
func parseLastModifiedMinute(h http.Header) time.Time {
	modRaw := h.Get("Last-Modified")
	if len(modRaw) == 0 {
		return time.Time{}
	}
	t, err := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", modRaw)
	if err != nil {
		return time.Time{}
	}
	return t.Truncate(time.Minute)
}
