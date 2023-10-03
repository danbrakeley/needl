//go:build mage

package main

import (
	"bytes"
	"strings"
	"time"

	"github.com/danbrakeley/bsh"
)

var (
	sh    = &bsh.Bsh{}
	needl = "needl"
)

// Build tests and builds the app (output goes to "local" folder)
func Build() {
	target := sh.ExeName(needl)

	sh.Echo("Running unit tests...")
	sh.Cmd("go test ./...").Run()

	sh.Echof("Building %s...", target)
	sh.MkdirAll("local/")

	// grab git commit hash to use as version for local builds
	commit := "(dev)"
	var b bytes.Buffer
	n := sh.Cmd(`git log --pretty=format:'%h' -n 1`).Out(&b).RunExitStatus()
	if n == 0 {
		commit = strings.TrimSpace(b.String())
	}

	sh.Cmdf(
		`go build -ldflags '`+
			`-X "github.com/danbrakeley/needl/internal/buildvar.Version=%s" `+
			`-X "github.com/danbrakeley/needl/internal/buildvar.BuildTime=%s" `+
			`-X "github.com/danbrakeley/needl/internal/buildvar.ReleaseURL=https://github.com/danbrakeley/needl"`+
			`' -o local/%s ./cmd/%s`, commit, time.Now().Format(time.RFC3339), target, needl,
	).Run()
}

// CfgClean removes local/needl.toml and local/scraper.toml
func CfgClean() {
	sh.Echo("Removing local/needl.toml and local/scraper.toml...")
	sh.RemoveAll("local/needl.toml")
	sh.RemoveAll("local/scrapers.toml")
}

// Cfg copies needl.toml and scrapers.toml into the local folder
func CfgCopy() {
	sh.Echo("Copying needl.toml and scrapers.toml to local...")
	sh.Copy("needl.toml", "local/needl.toml")
	sh.Copy("scrapers.toml", "local/scrapers.toml")
}

// CfgLocal copies needl.local.toml and scrapers.local.toml into local/needl.toml and local/scrapers.toml, respectively
func CfgLocal() {
	sh.Echo("Copying needl.local.toml and scrapers.local.toml to local/needl.toml and local/scrapers.toml...")
	sh.Copy("needl.local.toml", "local/needl.toml")
	sh.Copy("scrapers.local.toml", "local/scrapers.toml")
}

// Run whatever is in local
func Run() {
	target := sh.ExeName(needl)

	sh.Echo("Running...")
	sh.Cmdf("./%s", target).Dir("local").Run()
}
