package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const defaultProviderTimeout = 5 * time.Second

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

// LoadProviders loads jobs from all providers concurrently under ctx (each bounded
// by defaultProviderTimeout), recovers panics in Name() and Load(), enforces strict
// Source namespacing and (Source, ID) uniqueness, and merges in deterministic order.
func LoadProviders(ctx context.Context, providers []JobProvider) ProviderLoadResult {
	res := ProviderLoadResult{
		Statuses: make([]ProviderStatus, 0, len(providers)),
	}
	if len(providers) == 0 {
		return res
	}
	if ctx == nil {
		ctx = context.Background()
	}

	active := make([]JobProvider, 0, len(providers))
	for _, p := range providers {
		if p != nil {
			active = append(active, p)
		}
	}
	if len(active) == 0 {
		return res
	}

	outcomes := make([]singleProviderOutcome, len(active))
	var wg sync.WaitGroup
	for i, p := range active {
		wg.Add(1)
		go func(idx int, prov JobProvider) {
			defer wg.Done()
			pCtx, cancel := context.WithTimeout(ctx, defaultProviderTimeout)
			defer cancel()
			outcomes[idx] = loadSingleProviderSafe(pCtx, prov)
		}(i, p)
	}
	wg.Wait()

	seenKeys := make(map[JobKey]struct{})
	for _, out := range outcomes {
		if out.err != nil {
			res.Statuses = append(res.Statuses, ProviderStatus{
				Name:     out.name,
				Warnings: out.warnings,
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
			Warnings: out.warnings,
		})
	}
	return res
}

func loadSingleProviderSafe(ctx context.Context, p JobProvider) (out singleProviderOutcome) {
	out.name = "provider"
	defer func() {
		if r := recover(); r != nil {
			out.items = nil
			out.err = fmt.Errorf("provider panic: %v", r)
		}
	}()
	if n := sanitizeText(p.Name()); n != "" {
		out.name = n
	}
	if wp, ok := p.(WarningProvider); ok {
		out.items, out.warnings, out.err = wp.LoadWithWarnings(ctx)
		return out
	}
	out.items, out.err = p.Load(ctx)
	return out
}
