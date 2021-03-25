package main

import (
	"context"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/vmware/govmomi/vim25/mo"
	"knative.dev/pkg/logging"
)

const (
	cacheGCInterval = time.Second * 10 // periodically check for expired cache TTLs
)

type cache struct {
	clock clock.Clock
	ttl   int64
	sync.RWMutex
	cache map[string]*item
}

type item struct {
	alarm mo.Alarm
	added int64
}

func newAlarmCache(ttl int64) *cache {
	return &cache{
		clock: clock.New(),
		ttl:   ttl,
		cache: map[string]*item{},
	}
}

func (c *cache) add(key string, alarm mo.Alarm) {
	c.Lock()
	defer c.Unlock()
	c.cache[key] = &item{
		alarm: alarm,
		added: c.clock.Now().UTC().Unix(),
	}
}

func (c *cache) get(key string) (mo.Alarm, bool) {
	c.RLock()
	defer c.RUnlock()
	if k, ok := c.cache[key]; ok {
		return k.alarm, true
	}
	return mo.Alarm{}, false
}

func (c *cache) run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			logging.FromContext(ctx).Debugf("stopping alarm cache: %v", ctx.Err())
			return ctx.Err()
		case <-c.clock.Tick(cacheGCInterval):
			func() {
				c.Lock()
				defer c.Unlock()

				logging.FromContext(ctx).Debugf("purging stale cache items")
				for k, v := range c.cache {
					if c.clock.Now().UTC().Unix()-v.added > c.ttl {
						logging.FromContext(ctx).Debugf("removing stale cache item: %s", k)
						delete(c.cache, k)
					}
				}
			}()
		}
	}
}
