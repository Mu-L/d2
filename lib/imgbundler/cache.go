package imgbundler

import (
	"container/list"
	"sync"
)

// imageCache is a bounded, concurrency-safe LRU cache. Its byte ceiling charges
// key and value strings; the entry ceiling bounds fixed map/list overhead.
// Values are immutable image-element prefixes, so callers may share a hit
// without copying it.
type imageCache struct {
	mu         sync.Mutex
	entries    map[imageCacheKey]*imageCacheEntry
	recent     *list.List
	maxEntries int
	maxBytes   int64
	bytes      int64
}

type imageCacheEntry struct {
	key     imageCacheKey
	value   []byte
	bytes   int64
	element *list.Element
}

func newImageCache(maxEntries int, maxBytes int64) *imageCache {
	return &imageCache{
		entries:    make(map[imageCacheKey]*imageCacheEntry),
		recent:     list.New(),
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
	}
}

func (c *imageCache) Load(key imageCacheKey) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.recent.MoveToFront(entry.element)
	return entry.value, true
}

func (c *imageCache) Store(key imageCacheKey, value []byte) {
	if c == nil {
		return
	}
	entryBytes := imageCacheEntryBytes(key, value)

	c.mu.Lock()
	defer c.mu.Unlock()
	if previous, ok := c.entries[key]; ok {
		c.remove(previous)
	}
	if c.maxEntries <= 0 || c.maxBytes <= 0 || entryBytes > c.maxBytes {
		return
	}
	for len(c.entries) >= c.maxEntries || entryBytes > c.maxBytes-c.bytes {
		oldest := c.recent.Back()
		if oldest == nil {
			return
		}
		c.remove(oldest.Value.(*imageCacheEntry))
	}
	entry := &imageCacheEntry{key: key, value: value, bytes: entryBytes}
	entry.element = c.recent.PushFront(entry)
	c.entries[key] = entry
	c.bytes += entryBytes
}

func (c *imageCache) remove(entry *imageCacheEntry) {
	delete(c.entries, entry.key)
	c.recent.Remove(entry.element)
	c.bytes -= entry.bytes
}

func imageCacheEntryBytes(key imageCacheKey, value []byte) int64 {
	return int64(len(key.href)) + int64(len(key.localPolicyKey)) + int64(len(value)) + 2
}
