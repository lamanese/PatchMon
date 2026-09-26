package reports

import (
	"errors"
	"fmt"
)

// Archive retention per report: the worker keeps the newest ArchiveKeep runs
// of a report and deletes the rest when a run finishes.
const (
	// DefaultArchiveKeep is the retention of a report that never set one.
	DefaultArchiveKeep = 24
	// MinArchiveKeep is the smallest retention (the newest run always stays).
	MinArchiveKeep = 1
	// MaxArchiveKeep bounds the stored PDFs per report.
	MaxArchiveKeep = 200
)

// ErrArchiveKeep marks an archive_keep value outside MinArchiveKeep..MaxArchiveKeep.
var ErrArchiveKeep = errors.New("archive_keep")

// ValidateArchiveKeep checks a requested retention.
func ValidateArchiveKeep(n int) error {
	if n < MinArchiveKeep || n > MaxArchiveKeep {
		return fmt.Errorf("%w: must be between %d and %d runs", ErrArchiveKeep, MinArchiveKeep, MaxArchiveKeep)
	}
	return nil
}
