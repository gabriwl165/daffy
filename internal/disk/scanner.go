package disk

import (
	"context"
	"errors"
)

type scanner struct{ platform Platform }

func (s *scanner) Scan(context.Context, Config) (Report, error) {
	return Report{}, errors.New("disk scanner not initialized")
}
