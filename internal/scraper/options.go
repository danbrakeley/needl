package scraper

type Option interface {
	isScraperOption()
	String() string
}

// BaseURL

func BaseURL(v string) Option {
	return optBaseURL{v: v}
}

type optBaseURL struct {
	v string
}

func (_ optBaseURL) isScraperOption() {}
func (_ optBaseURL) String() string   { return "BaseURL" }
