package rproxymod

import (
	"time"
)

// Statitics about cache usage.
type CacheStats struct {
	CacheHitsCount   int64
	CacheMissesCount int64
	CacheErrorCount  int64
}

// CacheFetcher is used by the cache to retrieve a value for a key after a cache miss.
type CacheFetcher interface {
	Fetch(key []byte) (value []byte, ttl time.Duration, err error)
}

// Cache defines the interface of a cache the reverse proxy can use to cache cross-request information.
type Cache interface {
	// Set a cache value, potentially with a TTL
	Set(key []byte, value []byte, ttl time.Duration) error

	// Get the value of a key
	Get(key []byte) (value []byte, err error)

	// Get a value and fetch it in case of cache miss.
	GetAndStore(key []byte, fetcher CacheFetcher) (value []byte, err error)

	// Delete a key from the cache
	Delete(key []byte)

	// Clear the cache
	Clear()

	// Close the cache
	Close() error

	// Get statistics about cache usage.
	GetCacheStats() (stats *CacheStats)
}
