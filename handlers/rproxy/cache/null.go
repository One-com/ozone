package cache

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/One-com/ozone/rproxymod"
)

type NullCache struct {
	cacheErrorCount int64
}

func NewNullCache() (cacher *NullCache) {
	cacher = &NullCache{cacheErrorCount: 0}
	return
}

func (nc *NullCache) Set(key []byte, value []byte, ttl time.Duration) error {
	return fmt.Errorf("OP:Set(%s) Null cache configured, would always return error", string(key))
}

func (nc *NullCache) Get(key []byte) (value []byte, err error) {
	return nil, fmt.Errorf("OP:Get(%s) Null cache configured, would always return error", string(key))
}

func (nc *NullCache) GetAndStore(key []byte, fetcher rproxymod.CacheFetcher) (value []byte, err error) {
	value, _, err = fetcher.Fetch(key)
	if err != nil {
		atomic.AddInt64(&nc.cacheErrorCount, 1)
	}
	return
}

func (nc *NullCache) Delete(key []byte) {
	return
}

func (nc *NullCache) Clear() {
}

func (nc *NullCache) Close() error {
	return nil
}

func (nc *NullCache) GetCacheStats() (stats *rproxymod.CacheStats) {
	stats = new(rproxymod.CacheStats)
	stats.CacheErrorCount = atomic.LoadInt64(&nc.cacheErrorCount)
	return stats
}
