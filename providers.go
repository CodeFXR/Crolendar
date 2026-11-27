package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultProviderTimeout = 5 * time.Second
	maxProviderWarnings    = 20
)

// WarningProvider is an optional extension of JobProvider for adapters that
// can report non-fatal parse warnings (such as skipped malformed lines).
type WarningProvider interface {
	JobProvider
	LoadWithWarnings(ctx context.Context) ([]JobItem, []string, error)
}

// ProviderStatus reports the load outcome of a single JobProvider.
type ProviderStatus struct {
	Name     string
	Count    int
	Warnings []string
	Err      error
}

// ProviderLoadResult holds aggregated jobs and per-provider status from LoadProviders.
type ProviderLoadResult struct {
	Jobs     []JobItem
	Statuses []ProviderStatus
}

type singleProviderOutcome struct {
	name     string
	items    []JobItem
	warnings []string
	err      error
}

type providerSlot struct {
	provider JobProvider
	busy     atomic.Bool
	lastName atomic.Value // stores string
}

func (s *providerSlot) fallbackName(idx int) string {
	if v, ok := s.lastName.Load().(string); ok && v != "" {
		return v
	}
	return fmt.Sprintf("provider-%d", idx+1)
}

// ProviderLoader coordinates concurrent provider reloads across a fixed set of
// providers, ensuring a non-cooperative provider that outlives its timeout
// cannot accumulate additional leaked goroutines on subsequent reloads.
type ProviderLoader struct {
	slots []*providerSlot
}

// NewProviderLoader creates a ProviderLoader for the non-nil providers in order.
func NewProviderLoader(providers []JobProvider) *ProviderLoader {
	slots := make([]*providerSlot, 0, len(providers))
	for _, p := range providers {
		if p != nil {
			slots = append(slots, &providerSlot{provider: p})
		}
	}
	return &ProviderLoader{slots: slots}
}

// Load executes all configured providers concurrently under ctx with a hard
// per-provider deadline, panic recovery in Name() and Load(), warning capping
// and sanitization, strict Source namespacing, and (Source, ID) uniqueness.
func (l *ProviderLoader) Load(ctx context.Context) ProviderLoadResult {
	if l == nil || len(l.slots) == 0 {
		return ProviderLoadResult{}
	}
	res := ProviderLoadResult{
		Statuses: make([]ProviderStatus, 0, len(l.slots)),
	}
	if ctx == nil {
		ctx = context.Background()
	}

	outcomes := make([]singleProviderOutcome, len(l.slots))
	var wg sync.WaitGroup
	for i, slot := range l.slots {
		wg.Add(1)
		go func(idx int, s *providerSlot) {
			defer wg.Done()
			if !s.busy.CompareAndSwap(false, true) {
				outcomes[idx] = singleProviderOutcome{
					name: s.fallbackName(idx),
					err:  fmt.Errorf("provider timed out: previous load still running"),
				}
				return
			}

			pCtx, cancel := context.WithTimeout(ctx, defaultProviderTimeout)
			defer cancel()

			nameCh := make(chan string, 1)
			ch := make(chan singleProviderOutcome, 1)
			go func() {
				defer s.busy.Store(false)
				ch <- loadSingleProviderSafe(pCtx, s, nameCh)
			}()

			select {
			case out := <-ch:
				outcomes[idx] = out
			case <-pCtx.Done():
				name := s.fallbackName(idx)
				select {
				case n := <-nameCh:
					if n != "" {
						name = n
					}
				default:
				}
				outcomes[idx] = singleProviderOutcome{
					name: name,
					err:  fmt.Errorf("provider timed out: %w", pCtx.Err()),
				}
			}
		}(i, slot)
	}
	wg.Wait()

	seenKeys := make(map[JobKey]struct{})
	for _, out := range outcomes {
		warnings := sanitizeAndCapWarnings(out.warnings)
		if out.err != nil {
			res.Statuses = append(res.Statuses, ProviderStatus{
				Name:     out.name,
				Warnings: warnings,
				Err:      out.err,
			})
			continue
		}

		added := 0
		for _, item := range out.items {
			// Enforce provider namespace unconditionally so adapters cannot collide
			// with or spoof another provider's Source namespace.
			item.Source = out.name
			if !item.HasStableKey() {
				continue
			}
			if _, dup := seenKeys[item.Key()]; dup {
				baseID := item.ID
				for suffix := 2; ; suffix++ {
					item.ID = fmt.Sprintf("%s#%d", baseID, suffix)
					if _, taken := seenKeys[item.Key()]; !taken {
						break
					}
				}
			}
			seenKeys[item.Key()] = struct{}{}
			res.Jobs = append(res.Jobs, item)
			added++
		}

		res.Statuses = append(res.Statuses, ProviderStatus{
			Name:     out.name,
			Count:    added,
			Warnings: warnings,
		})
	}
	return res
}

// LoadProviders loads jobs from all providers concurrently under ctx using a
// fresh ProviderLoader.
func LoadProviders(ctx context.Context, providers []JobProvider) ProviderLoadResult {
	return NewProviderLoader(providers).Load(ctx)
}

func sanitizeAndCapWarnings(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	limit := maxProviderWarnings
	// Preserve a pre-capped provider slice (e.g., maxCrontabWarnings + 1 summary line)
	// while still enforcing a strict upper bound for any custom WarningProvider.
	if len(raw) <= limit+1 {
		out := make([]string, 0, len(raw))
		for _, w := range raw {
			if clean := sanitizeText(w); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	}
	out := make([]string, 0, limit+1)
	suppressed := 0
	for _, w := range raw {
		clean := sanitizeText(w)
		if clean == "" {
			continue
		}
		if len(out) < limit {
			out = append(out, clean)
		} else {
			suppressed++
		}
	}
	if suppressed > 0 {
		out = append(out, fmt.Sprintf("%d more warning(s) suppressed", suppressed))
	}
	return out
}

func loadSingleProviderSafe(ctx context.Context, s *providerSlot, nameCh chan<- string) (out singleProviderOutcome) {
	out.name = "provider"
	defer func() {
		if r := recover(); r != nil {
			out.items = nil
			out.err = fmt.Errorf("provider panic: %v", r)
		}
	}()
	if n := sanitizeText(s.provider.Name()); n != "" {
		out.name = n
		s.lastName.Store(n)
	}
	select {
	case nameCh <- out.name:
	default:
	}
	if wp, ok := s.provider.(WarningProvider); ok {
		out.items, out.warnings, out.err = wp.LoadWithWarnings(ctx)
		return out
	}
	out.items, out.err = s.provider.Load(ctx)
	return out
}
