package zorin

import (
	"github.com/gabriwl165/daffy/internal/distribution"
	"github.com/gabriwl165/daffy/internal/distribution/ubuntu"
)

func New() distribution.Profile {
	p := ubuntu.New()
	p.ID = distribution.Zorin
	return p
}
