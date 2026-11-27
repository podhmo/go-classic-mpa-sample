package gitrepo

import (
	"container/list"
	"sync"
)

// pullPairCacheSize bounds the immutable (baseSHA, headSHA) results kept
// per repository. Live detail pages and SSE fragments otherwise repeat the
// same log/merge probes on every refresh.
const pullPairCacheSize = 32

type pullPairCacheKey struct {
	baseSHA string
	headSHA string
}

type pullPairCacheEntry struct {
	key        pullPairCacheKey
	commits    []Commit
	hasCommits bool
	merge      *MergeCheck
}

type pullPairCache struct {
	mu      sync.Mutex
	entries map[pullPairCacheKey]*list.Element
	order   *list.List // front is most recently used
}

func cloneCommits(in []Commit) []Commit {
	return append([]Commit(nil), in...)
}

func cloneMergeCheck(in *MergeCheck) *MergeCheck {
	if in == nil {
		return nil
	}
	out := *in
	out.Files = append([]string(nil), in.Files...)
	return &out
}

func (c *pullPairCache) entry(key pullPairCacheKey) *list.Element {
	if c.entries == nil {
		c.entries = map[pullPairCacheKey]*list.Element{}
		c.order = list.New()
	}
	if el := c.entries[key]; el != nil {
		c.order.MoveToFront(el)
		return el
	}
	el := c.order.PushFront(&pullPairCacheEntry{key: key})
	c.entries[key] = el
	if len(c.entries) > pullPairCacheSize {
		old := c.order.Back()
		if old != nil {
			c.order.Remove(old)
			delete(c.entries, old.Value.(*pullPairCacheEntry).key)
		}
	}
	return el
}

func (c *pullPairCache) cachedCommits(key pullPairCacheKey) ([]Commit, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return nil, false
	}
	el := c.entries[key]
	if el == nil {
		return nil, false
	}
	c.order.MoveToFront(el)
	e := el.Value.(*pullPairCacheEntry)
	if !e.hasCommits {
		return nil, false
	}
	return cloneCommits(e.commits), true
}

func (c *pullPairCache) putCommits(key pullPairCacheKey, commits []Commit) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entry(key).Value.(*pullPairCacheEntry)
	e.commits = cloneCommits(commits)
	e.hasCommits = true
}

func (c *pullPairCache) cachedMergeCheck(key pullPairCacheKey) (*MergeCheck, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return nil, false
	}
	el := c.entries[key]
	if el == nil {
		return nil, false
	}
	c.order.MoveToFront(el)
	e := el.Value.(*pullPairCacheEntry)
	if e.merge == nil {
		return nil, false
	}
	return cloneMergeCheck(e.merge), true
}

func (c *pullPairCache) putMergeCheck(key pullPairCacheKey, check *MergeCheck) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entry(key).Value.(*pullPairCacheEntry)
	e.merge = cloneMergeCheck(check)
}
