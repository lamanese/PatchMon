package reports

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRenderGateSecondCallerIsBusyUntilRelease(t *testing.T) {
	g := NewRenderGate()
	rel1, err := g.Acquire(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Acquire(context.Background(), 30*time.Millisecond); !errors.Is(err, ErrRendererBusy) {
		t.Fatalf("second acquire: want ErrRendererBusy, got %v", err)
	}
	rel1()
	rel1() // idempotent
	rel2, err := g.Acquire(context.Background(), 30*time.Millisecond)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	rel2()
}

func TestRenderGateHonoursContextCancel(t *testing.T) {
	g := NewRenderGate()
	rel, _ := g.Acquire(context.Background(), 0)
	defer rel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := g.Acquire(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRenderGateNoWaitLimitWaitsForSlot(t *testing.T) {
	g := NewRenderGate()
	rel, _ := g.Acquire(context.Background(), 0)
	go func() { time.Sleep(20 * time.Millisecond); rel() }()
	rel2, err := g.Acquire(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	rel2()
}
