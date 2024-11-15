package utils

import "container/heap"

// Entry is a single element in the heap-map.
// It contains a key, value, priority and an index.
// K is the key type, V is the value type and P is the priority type.
type Entry[K comparable, V, P any] struct {
	Key      K
	Value    V
	Priority P
	i        uint
}

// HeapMap is a priority queue implemented as a heap-map.
// It maps keys to values and keeps them sorted by priority.
// K is the key type, V is the value type and P is the priority type.
type HeapMap[K comparable, V, P any] interface {
	// Peek returns the element with the highest priority.
	Peek() (Entry[K, V, P], bool)
	// Push adds a new element to the heap-map, overwriting the existing one if the key already exists.
	Push(key K, value V, priority P)
	// Pop removes and returns the element with the highest priority.
	Pop() (Entry[K, V, P], bool)

	// Get returns the element with the given key.
	Get(key K) (Entry[K, V, P], bool)
	// Remove removes the element with the given key.
	Remove(key K)
}

type heapMap[K comparable, V, P any] struct {
	heap  byPriority[K, V, P]
	index map[K]*Entry[K, V, P]
}

func NewHeapMap[K comparable, V, P any](priorityComparator func(P, P) bool) HeapMap[K, V, P] {
	return &heapMap[K, V, P]{
		heap:  byPriority[K, V, P]{less: priorityComparator},
		index: make(map[K]*Entry[K, V, P]),
	}
}

func (hm *heapMap[K, V, P]) Peek() (Entry[K, V, P], bool) {
	if len(hm.index) == 0 {
		return Entry[K, V, P]{}, false
	}

	return *hm.heap.entries[0], true
}

func (hm *heapMap[K, V, P]) Pop() (Entry[K, V, P], bool) {
	if len(hm.index) == 0 {
		return Entry[K, V, P]{}, false
	}

	entry := *heap.Pop(&hm.heap).(*Entry[K, V, P])
	delete(hm.index, entry.Key)
	return entry, true
}

func (hm *heapMap[K, V, P]) Push(key K, value V, priority P) {
	if entry, ok := hm.index[key]; ok {
		entry.Value = value
		entry.Priority = priority
		heap.Fix(&hm.heap, int(entry.i))
		return
	}

	entry := &Entry[K, V, P]{Key: key, Value: value, Priority: priority}
	heap.Push(&hm.heap, entry)
	hm.index[key] = entry
}

func (hm *heapMap[K, V, P]) Get(key K) (Entry[K, V, P], bool) {
	if entry, ok := hm.index[key]; ok {
		return *entry, true
	}

	return Entry[K, V, P]{}, false
}

func (hm *heapMap[K, V, P]) Remove(key K) {
	if entry, ok := hm.index[key]; ok {
		heap.Remove(&hm.heap, int(entry.i))
		delete(hm.index, key)
	}
}

// implementation of heap.Interface for HeapMap to store entries by priority
type byPriority[K comparable, V, P any] struct {
	entries []*Entry[K, V, P]
	less    func(P, P) bool
}

func (h byPriority[K, V, P]) Len() int {
	return len(h.entries)
}

func (h byPriority[K, V, P]) Less(i, j int) bool {
	return h.less(h.entries[i].Priority, h.entries[j].Priority)
}

func (h byPriority[K, V, P]) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	h.entries[i].i = uint(i)
	h.entries[j].i = uint(j)
}

func (h *byPriority[K, V, P]) Push(x any) {
	n := len(h.entries)
	entry := x.(*Entry[K, V, P])
	entry.i = uint(n)
	h.entries = append(h.entries, entry)
}

func (h *byPriority[K, V, P]) Pop() any {
	old := h.entries
	n := len(old)
	entry := old[n-1]
	h.entries = old[0 : n-1]
	return entry
}
