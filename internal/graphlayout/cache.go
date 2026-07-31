package graphlayout

import (
	"context"
	"fmt"

	"codeflux.dev/codeflux/internal/domain"
)

// CacheKey scopes persisted placement hints to one immutable graph revision
// and one layout algorithm. SQLite implementations belong to the storage
// layer; the pure layout package depends only on this bounded port.
type CacheKey struct {
	GraphID          domain.GraphID
	GraphRevisionID  domain.GraphRevisionID
	AlgorithmVersion string
}

func (key CacheKey) Validate() error {
	if key.GraphID.IsZero() || key.GraphRevisionID.IsZero() || key.AlgorithmVersion == "" {
		return fmt.Errorf("%w: cache key requires graph, revision, and algorithm identities", ErrInvalidLayoutInput)
	}
	return nil
}

type CachedLayout struct {
	Key    CacheKey
	Layout Layout
}

func (cached CachedLayout) Validate() error {
	if err := cached.Key.Validate(); err != nil {
		return err
	}
	if cached.Layout.AlgorithmVersion != cached.Key.AlgorithmVersion {
		return fmt.Errorf("%w: cached layout algorithm does not match its key", ErrInvalidLayoutInput)
	}
	return nil
}

type Cache interface {
	LoadLayout(context.Context, CacheKey) (CachedLayout, bool, error)
	StoreLayout(context.Context, CachedLayout) error
}
