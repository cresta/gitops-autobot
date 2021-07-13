package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Cache interface {
	// GetOrSet will fetch the value at 'key' as []byte and encode it into into.  If key does not exist, it will call the
	// data function for []byte, store that into key, and decode it into into
	GetOrSet(ctx context.Context, key []byte, ttl time.Duration, into interface{}, data func(ctx context.Context) (interface{}, error)) error
	// Delete the cached value at key
	Delete(ctx context.Context, key []byte) error
}

type inMemoryKey struct {
	expireAt time.Time
	val      []byte
}
type InMemoryCache struct {
	cache map[string]inMemoryKey
	mu    sync.Mutex
}

func (i *InMemoryCache) GetOrSet(ctx context.Context, key []byte, ttl time.Duration, into interface{}, data func(ctx context.Context) (interface{}, error)) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cache == nil {
		i.cache = make(map[string]inMemoryKey)
	}
	now := time.Now()
	if existingItem, exists := i.cache[string(key)]; exists {
		if existingItem.expireAt.After(now) {
			if err := json.Unmarshal(existingItem.val, into); err != nil {
				return fmt.Errorf("unable to unmarshal value in cache: %w", err)
			}
			return nil
		}
		delete(i.cache, string(key))
	}

	val, err := data(ctx)
	if err != nil {
		return fmt.Errorf("unable to fetch data for cache: %w", err)
	}
	encoded, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("unable to marshal returned value: %w", err)
	}
	item := inMemoryKey{
		expireAt: now.Add(ttl),
		val:      encoded,
	}
	if err := json.Unmarshal(item.val, into); err != nil {
		return fmt.Errorf("unable to unmarshal returned value from data: %w", err)
	}
	i.cache[string(key)] = item
	return nil
}

func (i *InMemoryCache) Delete(_ context.Context, key []byte) error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cache == nil {
		i.cache = make(map[string]inMemoryKey)
	}
	delete(i.cache, string(key))
	return nil
}

var _ Cache = &InMemoryCache{}
