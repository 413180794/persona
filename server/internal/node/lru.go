package node

import "container/list"

// lru is a map of at most max entries that drops the least recently used one to make room: a long-lived node's
// caches keep what the session list keeps asking for, instead of starting over when they fill up. Not safe for
// concurrent use: callers hold their own lock.
type lru[K comparable, V any] struct {
	max   int
	items map[K]*list.Element
	order list.List // front: most recently used
}

type lruItem[K comparable, V any] struct {
	key K
	val V
}

func newLRU[K comparable, V any](max int) *lru[K, V] {
	return &lru[K, V]{max: max, items: map[K]*list.Element{}}
}

// get is k's value, now the most recently used.
func (c *lru[K, V]) get(k K) (V, bool) {
	el, ok := c.items[k]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruItem[K, V]).val, true
}

// peek is k's value, leaving the order as it is (tests).
func (c *lru[K, V]) peek(k K) (V, bool) {
	el, ok := c.items[k]
	if !ok {
		var zero V
		return zero, false
	}
	return el.Value.(*lruItem[K, V]).val, true
}

// put sets k's value, the most recently used, dropping the least recently used entry when there are more than max.
func (c *lru[K, V]) put(k K, v V) {
	if el, ok := c.items[k]; ok {
		el.Value.(*lruItem[K, V]).val = v
		c.order.MoveToFront(el)
		return
	}
	c.items[k] = c.order.PushFront(&lruItem[K, V]{key: k, val: v})
	for c.max > 0 && c.order.Len() > c.max {
		el := c.order.Back()
		c.order.Remove(el)
		delete(c.items, el.Value.(*lruItem[K, V]).key)
	}
}

func (c *lru[K, V]) len() int { return c.order.Len() }
