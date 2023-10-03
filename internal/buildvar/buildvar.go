package buildvar

// -ldflags '-X "github.com/danbrakeley/needl/internal/buildvar.Version=${{ github.event.release.tag_name }}"'
var Version string

// -ldflags '-X "github.com/danbrakeley/needl/internal/buildvar.BuildTime=${{ github.event.release.created_at }}"'
var BuildTime string

// -ldflags '-X "github.com/danbrakeley/needl/internal/buildvar.ReleaseURL=${{ github.event.release.html_url }}"'
var ReleaseURL string
