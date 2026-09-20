package migrate

import (
	"context"
	"log/slog"
)

// forkBaseVersion is the last migration the fork shares with upstream. All 80
// files up to 000040 are byte-identical between the fork and upstream v2.1.3.
//
//nolint:unused // consumed by makeLegacyForkDB and the real bridge logic added in task 2
const forkBaseVersion = 40

func bridgeLegacyFork(_ context.Context, _ string, _ *slog.Logger) error {
	return nil
}
