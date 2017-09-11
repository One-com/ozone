package cache

import (
	"fmt"
	"github.com/valyala/ybc/bindings/go/ybc"

	"github.com/One-com/gone/jconf"

	"sync"
	"sync/atomic"
	"time"

	"github.com/One-com/ozone/rproxymod"
)

type YbcConfig struct {
	MaxItemSize   int
	MaxItemsCount int
}

// Cache imlementation based on 'valyala/ybc' library.
type YbcCache struct {
	cacher *ybc.SimpleCache
	// To store concurrent fetch calls
	fetchingLock sync.Mutex
	fetchings    map[string]*call

	cacheHitsCount   int64
	cacheMissesCount int64
	cacheErrorCount  int64
}

// call is an in-flight or completed Fetch call
type call struct {
	wg  sync.WaitGroup
	val []byte
	err error
}

func NewYbcCache(cfg jconf.SubConfig) (cacher *YbcCache, err error) {
	var yc *YbcConfig = &YbcConfig{
		MaxItemSize:   1024,
		MaxItemsCount: 64000,
	}
	err = cfg.ParseInto(yc)
	if err != nil {
		return
	}
	config := ybc.Config{
		MaxItemsCount: ybc.SizeT(yc.MaxItemsCount),
	}

	ybcCache, err := config.OpenSimpleCache(true)
	if err != nil {
		return
	}

	cacher = &YbcCache{
		cacher:    ybcCache,
		fetchings: make(map[string]*call),
	}
	return
}

func (yc *YbcCache) Set(key []byte, value []byte, ttl time.Duration) error {
	return yc.cacher.Set(key, value, ttl)
}

func (yc *YbcCache) Get(key []byte) (value []byte, err error) {
	value, err = yc.cacher.Get(key)
	if err == nil {
		atomic.AddInt64(&yc.cacheHitsCount, 1)
	} else {
		if err == ybc.ErrCacheMiss {
			atomic.AddInt64(&yc.cacheMissesCount, 1)
		} else {
			atomic.AddInt64(&yc.cacheErrorCount, 1)
		}
	}
	return
}

func (yc *YbcCache) GetAndStore(key []byte, fetcher rproxymod.CacheFetcher) (value []byte, err error) {
	value, err = yc.cacher.Get(key)
	if err == nil {
		atomic.AddInt64(&yc.cacheHitsCount, 1)
		return
	}
	if err != ybc.ErrCacheMiss {
		atomic.AddInt64(&yc.cacheErrorCount, 1)
		// :NOTE: Here, there is a probable case of cache corruption, this may require clearing up the cache altogather
		return nil, fmt.Errorf("Unexpected error when obtaining cache value for key [%v] [%s]", key, err.Error())
	}

	atomic.AddInt64(&yc.cacheMissesCount, 1)

	// Check if a lookup is being underway, protect against thundering-hurd/dog-pile-effect.
	strKey := string(key)
	yc.fetchingLock.Lock()
	c, fetch_exists := yc.fetchings[strKey]
	if fetch_exists {
		// if lookup query underway wait for result
		yc.fetchingLock.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}

	c = new(call)
	c.wg.Add(1)
	yc.fetchings[strKey] = c

	yc.fetchingLock.Unlock()

	var ttl time.Duration
	// Do fetch and store for subsequent calls
	c.val, ttl, c.err = fetcher.Fetch(key)

	// Add result to inprocess hot-cache
	if c.err == nil && c.val != nil {
		yc.cacher.Set(key, c.val, ttl)
	}

	// signal waiting lookups to resume
	c.wg.Done()

	yc.fetchingLock.Lock()
	delete(yc.fetchings, strKey)
	yc.fetchingLock.Unlock()

	return c.val, c.err
}

func (yc *YbcCache) Delete(key []byte) {
	yc.cacher.Delete(key)
}

func (yc *YbcCache) Clear() {
	yc.fetchingLock.Lock()
	yc.fetchings = make(map[string]*call)
	yc.fetchingLock.Unlock()
	yc.cacher.Clear()
}

func (yc *YbcCache) Close() error {
	return yc.cacher.Close()
}

func (yc *YbcCache) GetCacheStats() (stats *rproxymod.CacheStats) {
	stats = new(rproxymod.CacheStats)
	stats.CacheErrorCount = atomic.LoadInt64(&yc.cacheErrorCount)
	stats.CacheHitsCount = atomic.LoadInt64(&yc.cacheHitsCount)
	stats.CacheMissesCount = atomic.LoadInt64(&yc.cacheMissesCount)
	return
}
