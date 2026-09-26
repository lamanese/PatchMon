package reports

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrRendererBusy is returned when the render slot is not free within maxWait.
var ErrRendererBusy = errors.New("renderer busy")

const (
	// PreviewWait is how long an interactive preview waits for the slot.
	PreviewWait = 10 * time.Second
	// RenderDeadline bounds one PDF rendering (API group timeout is 30 s).
	RenderDeadline = 25 * time.Second
)

// RenderGate serialises PDF rendering: one document at a time per process.
type RenderGate struct{ sem chan struct{} }

// NewRenderGate returns a gate with one slot.
func NewRenderGate() *RenderGate { return &RenderGate{sem: make(chan struct{}, 1)} }

// DefaultGate is shared by the preview endpoint and the report worker.
var DefaultGate = NewRenderGate()

// Acquire takes the slot. maxWait <= 0 waits until ctx ends.
func (g *RenderGate) Acquire(ctx context.Context, maxWait time.Duration) (func(), error) {
	var timeout <-chan time.Time
	if maxWait > 0 {
		t := time.NewTimer(maxWait)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case g.sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-g.sem }) }, nil
	case <-timeout:
		return nil, ErrRendererBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
