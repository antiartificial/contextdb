package embedding

import (
	"context"
	"crypto/sha256"
	"sync"
)

// Cached wraps an Embedder with an LRU cache that avoids re-embedding
// identical text. Thread-safe.
type Cached struct {
	inner    Embedder
	mu       sync.RWMutex
	cache    map[[32]byte][]float32
	order    [][32]byte // insertion order for eviction
	maxItems int
}

// NewCached wraps inner with an LRU cache of up to maxItems entries.
func NewCached(inner Embedder, maxItems int) *Cached {
	if maxItems <= 0 {
		maxItems = 4096
	}
	return &Cached{
		inner:    inner,
		cache:    make(map[[32]byte][]float32, maxItems),
		order:    make([][32]byte, 0, maxItems),
		maxItems: maxItems,
	}
}

func (c *Cached) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))
	var uncached []string
	var uncachedIdx []int

	c.mu.RLock()
	for i, t := range texts {
		key := sha256.Sum256([]byte(t))
		if vec, ok := c.cache[key]; ok {
			results[i] = vec
		} else {
			uncached = append(uncached, t)
			uncachedIdx = append(uncachedIdx, i)
		}
	}
	c.mu.RUnlock()

	if len(uncached) == 0 {
		return results, nil
	}

	vecs, err := c.inner.Embed(ctx, uncached)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	for j, vec := range vecs {
		results[uncachedIdx[j]] = vec
		key := sha256.Sum256([]byte(uncached[j]))
		if _, exists := c.cache[key]; !exists {
			if len(c.order) >= c.maxItems {
				// evict oldest
				evict := c.order[0]
				c.order = c.order[1:]
				delete(c.cache, evict)
			}
			c.order = append(c.order, key)
		}
		c.cache[key] = vec
	}
	c.mu.Unlock()

	return results, nil
}

func (c *Cached) Dimensions() int {
	return c.inner.Dimensions()
}
