package connectors_test

import (
	"sync"
	"testing"

	"github.com/icofcucam/naditos/packages/go-common/connectors"
)

// TestRouter_Default: with only a default bound, every tenant resolves
// to that one. This is the dev-stub configuration: one adapter for
// everyone, swapped in for production by per-tenant Bind calls.
func TestRouter_Default(t *testing.T) {
	r := connectors.NewRouter[string]()
	r.SetDefault("default-stub")

	for _, tenant := range []string{"a", "b", "c"} {
		got, err := r.For(tenant)
		if err != nil {
			t.Fatalf("%s: %v", tenant, err)
		}
		if got != "default-stub" {
			t.Fatalf("%s: want default-stub, got %s", tenant, got)
		}
	}
}

// TestRouter_TenantOverridesDefault: a per-tenant binding wins over
// the default. Real production case: tenant X uses a sovereign
// provider, everyone else uses a regional one.
func TestRouter_TenantOverridesDefault(t *testing.T) {
	r := connectors.NewRouter[string]()
	r.SetDefault("default")
	r.Bind("special", "tenant-only")

	if got, _ := r.For("special"); got != "tenant-only" {
		t.Fatalf("special: want tenant-only, got %s", got)
	}
	if got, _ := r.For("other"); got != "default" {
		t.Fatalf("other: want default, got %s", got)
	}
}

// TestRouter_NoBinding_NoDefault: with no default and no binding,
// For returns a typed error so the handler can 503.
func TestRouter_NoBinding_NoDefault(t *testing.T) {
	r := connectors.NewRouter[string]()
	_, err := r.For("anyone")
	if err == nil {
		t.Fatal("want error when no provider bound, got nil")
	}
}

// TestRouter_ConcurrentBindAndFor: Bind and For are race-free.
// CountryRouter is read every request and may be written by an
// admin tool; the RWMutex must hold up under -race.
func TestRouter_ConcurrentBindAndFor(t *testing.T) {
	r := connectors.NewRouter[string]()
	r.SetDefault("d")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			r.Bind("t", "p")
		}(i)
		go func() {
			defer wg.Done()
			_, _ = r.For("t")
		}()
	}
	wg.Wait()
}
