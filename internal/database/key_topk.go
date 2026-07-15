package database

import (
	"container/heap"
	"sort"
	"strings"
	"sync"
)

type KeyCount struct {
	Key   string
	Count uint64
}

type keyEntry struct {
	key   string
	count uint64
	err   uint64
	index int
}

type keyMinHeap []*keyEntry

func (h keyMinHeap) Len() int { return len(h) }

func (h keyMinHeap) Less(i, j int) bool {
	if h[i].count != h[j].count {
		return h[i].count < h[j].count
	}
	return h[i].key < h[j].key
}

func (h keyMinHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *keyMinHeap) Push(x interface{}) {
	e := x.(*keyEntry)
	e.index = len(*h)
	*h = append(*h, e)
}

func (h *keyMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	e := old[n-1]
	e.index = -1
	*h = old[:n-1]
	return e
}

type KeyTopK struct {
	mu sync.Mutex
	k  int
	m  map[string]*keyEntry
	h  keyMinHeap
}

func NewKeyTopK(k int) *KeyTopK {
	if k < 0 {
		k = 0
	}
	t := &KeyTopK{
		k: k,
		m: make(map[string]*keyEntry, k),
		h: make(keyMinHeap, 0, k),
	}
	heap.Init(&t.h)
	return t
}

func (t *KeyTopK) Add(key string) {
	if key == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.k == 0 {
		return
	}

	if e, ok := t.m[key]; ok {
		e.count++
		heap.Fix(&t.h, e.index)
		return
	}

	if len(t.h) < t.k {
		key = strings.Clone(key)
		e := &keyEntry{key: key, count: 1}
		t.m[key] = e
		heap.Push(&t.h, e)
		return
	}

	min := t.h[0]
	delete(t.m, min.key)
	key = strings.Clone(key)
	min.key = key
	min.err = min.count
	min.count = min.count + 1
	t.m[key] = min
	heap.Fix(&t.h, 0)
}

func (t *KeyTopK) Top(n int) []KeyCount {
	t.mu.Lock()
	defer t.mu.Unlock()

	if n <= 0 {
		return nil
	}
	if n > len(t.h) {
		n = len(t.h)
	}

	out := make([]KeyCount, 0, len(t.h))
	for _, e := range t.h {
		out = append(out, KeyCount{Key: e.key, Count: e.count})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})

	return out[:n]
}

func (t *KeyTopK) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.m = make(map[string]*keyEntry, t.k)
	t.h = make(keyMinHeap, 0, t.k)
	heap.Init(&t.h)
}
