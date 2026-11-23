package compiler

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRCUCachePerKey(t *testing.T) {
	var cache rcuCache[string, int]
	started, release := make(chan struct{}), make(chan struct{})
	var builds atomic.Int32
	build := func() (int, error) {
		builds.Add(1)
		close(started)
		<-release
		return 42, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := cache.compile("same", build)
			if value != 42 || err != nil {
				t.Errorf("got %d, %v", value, err)
			}
		}()
	}
	<-started
	other := make(chan struct{})
	go func() {
		defer close(other)
		if value, err := cache.compile("other", func() (int, error) { return 7, nil }); value != 7 || err != nil {
			t.Errorf("other compile: %d, %v", value, err)
		}
	}()
	select {
	case <-other:
	case <-time.After(5 * time.Second):
		t.Error("unrelated type was blocked by an in-progress compile")
	}
	close(release)
	wg.Wait()
	if builds.Load() != 1 {
		t.Fatalf("compiled same type %d times", builds.Load())
	}
	if value, ok := cache.getCompileResult("same"); !ok || value != 42 {
		t.Fatal("successful result not published")
	}
}

func TestRCUCacheRetainsErrors(t *testing.T) {
	var cache rcuCache[int, int]
	want := errors.New("unsupported")
	builds := 0
	for i := 0; i < 2; i++ {
		_, err := cache.compile(1, func() (int, error) { builds++; return 0, want })
		if !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
	if builds != 1 {
		t.Fatalf("failed type compiled %d times", builds)
	}
}
