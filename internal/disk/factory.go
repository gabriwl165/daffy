package disk

import "context"

func Scan(ctx context.Context, c Config) (Report, error) { return NewScanner().Scan(ctx, c) }
func NewScanner() Scanner                                { return newScanner() }
