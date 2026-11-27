package main

import (
	"context"
	"fmt"
	"time"
)

const defaultProviderTimeout = 5 * time.Second

// ProviderStatus reports the load outcome of a single JobProvider.
type ProviderStatus struct {
	Name  string
	Count int
	Err   error
}

// ProviderLoadResult holds aggregated jobs and per-provider status from LoadProviders.
type ProviderLoadResult struct {
	Jobs     []JobItem
	Statuses []ProviderStatus
}

// LoadProviders loads jobs from all providers sequentially under ctx, enforces
// source namespacing and (Source, ID) uniqueness, and isolates per-provider errors.
func LoadProviders(ctx context.Context, providers []JobProvider) ProviderLoadResult {
	res := ProviderLoadResult{
		Statuses: make([]ProviderStatus, 0, len(providers)),
	}
	if len(providers) == 0 {
		return res
	}

	seenKeys := make(map[JobKey]struct{})
	for _, p := range providers {
		if p == nil {
			continue
		}
		name := sanitizeText(p.Name())
		if name == "" {
			name = "provider"
		}

		pCtx, cancel := context.WithTimeout(ctx, defaultProviderTimeout)
		items, err := loadSingleProviderSafe(pCtx, p)
		cancel()

		if err != nil {
			res.Statuses = append(res.Statuses, ProviderStatus{
				Name: name,
				Err:  err,
			})
			continue
		}

		added := 0
		for _, item := range items {
			if item.Source == "" {
				item.Source = name
			}
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
			Name:  name,
			Count: added,
		})
	}
	return res
}

func loadSingleProviderSafe(ctx context.Context, p JobProvider) (items []JobItem, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("provider panic: %v", r)
		}
	}()
	return p.Load(ctx)
}
