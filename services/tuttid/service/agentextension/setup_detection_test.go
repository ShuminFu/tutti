package agentextension

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockedSetupInstallationStore struct {
	InstallationStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	reads   atomic.Int32
}

func (s *blockedSetupInstallationStore) ReadInstallation(id string) (Installation, error) {
	s.reads.Add(1)
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.InstallationStore.ReadInstallation(id)
}

type setupWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *setupWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestSetupDetectionSharesWorkAndSeparatesCallerCancellation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	service, targetID := setupFixture(t, "generic", "Generic", "@example/generic", "1.2.3", "generic-agent", ">=1.2.3", nil, &probeTransport{})
	store := &blockedSetupInstallationStore{InstallationStore: service.Plans.Manager.Installations, entered: make(chan struct{}), release: make(chan struct{})}
	service.Plans.Manager.Installations = store
	// Always release the fixture before service cleanup waits for its worker.
	var release sync.Once
	defer release.Do(func() { close(store.release) })
	input := InstallPlanInput{WorkspaceID: "workspace-1", AgentTargetID: targetID}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	first := make(chan error, 1)
	go func() { _, err := service.GetSetup(firstCtx, input); first <- err }()
	select {
	case <-store.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	secondCtx := &setupWaitingContext{Context: ctx, waiting: make(chan struct{})}
	second := make(chan error, 1)
	go func() { _, err := service.GetSetup(secondCtx, input); second <- err }()
	select {
	case <-secondCtx.waiting:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancelFirst()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller: %v", err)
	}
	release.Do(func() { close(store.release) })
	if err := <-second; err != nil {
		t.Fatalf("other caller lost shared detection: %v", err)
	}
	if got := store.reads.Load(); got != 1 {
		t.Fatalf("concurrent setup read installation %d times", got)
	}
	if _, err := service.GetSetup(ctx, input); err != nil {
		t.Fatal(err)
	}
	if got := store.reads.Load(); got != 2 {
		t.Fatalf("manual retry did not detect again: reads=%d", got)
	}
}
