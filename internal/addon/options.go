package addon

import (
	"github.com/dbytex91/streamx/internal/debrid/realdebrid"
	"github.com/dbytex91/streamx/internal/prowlarr"
)

func WithID(id string) Option {
	return func(a *Addon) {
		a.id = id
	}
}

func WithName(name string) Option {
	return func(a *Addon) {
		a.name = name
	}
}

func WithProwlarr(jacketUrl string, jacketApiKey string) Option {
	return func(a *Addon) {
		a.prowlarrClient = prowlarr.New(jacketUrl, jacketApiKey)
		a.prowlarrURL = jacketUrl
		a.prowlarrAPIKey = jacketApiKey
	}
}

func WithRealDebrid(apiKey string) Option {
	return func(a *Addon) {
		a.realDebridClient = realdebrid.New(apiKey, "")
		a.realDebridAPIKey = apiKey
	}
}


func WithVersion(version string) Option {
	return func(a *Addon) {
		a.version = version
	}
}
