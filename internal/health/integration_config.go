package health

import (
	"github.com/gabriwl165/daffy/internal/distribution"
	"time"
)

type commandConfig struct {
	Timeout time.Duration
	Tool    distribution.Tool
	Args    []string
}

type journalBoot int

const (
	journalCurrent journalBoot = iota
	journalPrevious
)

func (b journalBoot) config(timeout time.Duration) (commandConfig, string) {
	flag, label := "--boot", "current"
	if b == journalPrevious {
		flag, label = "--boot=-1", "previous"
	}
	return commandConfig{Timeout: timeout, Tool: distribution.Journalctl, Args: []string{"--no-pager", "--quiet", "--lines=1500", "-k", flag, "-o", "cat"}}, label
}

func journalFindingID(b journalBoot) string {
	if b == journalPrevious {
		return findingJournalPrevious
	}
	return findingJournalCurrent
}
