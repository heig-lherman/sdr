package utils

import (
	"testing"
)

func TestHeapMapEmptyOperations(t *testing.T) {
	hm := NewHeapMap[string, int, int](func(a, b int) bool { return a < b })

	// Test Peek on empty heap
	if _, ok := hm.Peek(); ok {
		t.Error("Peek on empty heap should return false")
	}

	// Test Pop on empty heap
	if _, ok := hm.Pop(); ok {
		t.Error("Pop on empty heap should return false")
	}

	// Test Get on empty heap
	if _, ok := hm.Get("nonexistent"); ok {
		t.Error("Get on empty heap should return false")
	}

	// Test Remove on empty heap (should not panic)
	hm.Remove("nonexistent")
}

func TestHeapMapBasicOperations(t *testing.T) {
	hm := NewHeapMap[string, string, int](func(a, b int) bool { return a < b }) // Min heap

	// Test single Push and Get
	hm.Push("a", "value-a", 1)
	entry, ok := hm.Get("a")
	if !ok || entry.Value != "value-a" || entry.Priority != 1 {
		t.Error("Push/Get failed for single element")
	}

	// Test Peek
	peek, ok := hm.Peek()
	if !ok || peek.Key != "a" || peek.Value != "value-a" || peek.Priority != 1 {
		t.Error("Peek failed to return correct element")
	}

	// Test Pop
	pop, ok := hm.Pop()
	if !ok || pop.Key != "a" || pop.Value != "value-a" || pop.Priority != 1 {
		t.Error("Pop failed to return correct element")
	}
}

func TestHeapMapPriorityOrdering(t *testing.T) {
	// Test with min heap
	hm := NewHeapMap[string, string, int](func(a, b int) bool { return a < b })

	elements := []struct {
		key      string
		value    string
		priority int
	}{
		{"c", "value-c", 3},
		{"a", "value-a", 1},
		{"b", "value-b", 2},
	}

	// Push elements
	for _, e := range elements {
		hm.Push(e.key, e.value, e.priority)
	}

	// Verify order through Pop
	expected := []string{"a", "b", "c"}
	for _, exp := range expected {
		entry, ok := hm.Pop()
		if !ok || entry.Key != exp {
			t.Errorf("Expected key %s, got %s", exp, entry.Key)
		}
	}
}

func TestHeapMapUpdatePriority(t *testing.T) {
	hm := NewHeapMap[string, string, int](func(a, b int) bool { return a < b })

	// Initial push
	hm.Push("a", "value-a", 3)

	// Update with lower priority
	hm.Push("a", "value-a-updated", 1)

	entry, ok := hm.Get("a")
	if !ok || entry.Priority != 1 || entry.Value != "value-a-updated" {
		t.Error("Priority update failed")
	}

	// Add more elements and verify order
	hm.Push("b", "value-b", 2)
	hm.Push("c", "value-c", 3)

	// First element should be "a" with priority 1
	peek, ok := hm.Peek()
	if !ok || peek.Key != "a" || peek.Priority != 1 {
		t.Error("Incorrect ordering after priority update")
	}
}

func TestHeapMapRemove(t *testing.T) {
	hm := NewHeapMap[string, string, int](func(a, b int) bool { return a < b })

	// Add elements
	hm.Push("a", "value-a", 1)
	hm.Push("b", "value-b", 2)
	hm.Push("c", "value-c", 3)

	// Remove middle element
	hm.Remove("b")

	// Verify "b" is gone
	if _, ok := hm.Get("b"); ok {
		t.Error("Element 'b' should have been removed")
	}

	// Verify remaining elements are in correct order
	first, _ := hm.Pop()
	second, _ := hm.Pop()

	if first.Key != "a" || second.Key != "c" {
		t.Error("Incorrect order after removal")
	}
}

func TestHeapMapMaxHeap(t *testing.T) {
	// Test with max heap
	hm := NewHeapMap[string, string, int](func(a, b int) bool { return a > b })

	elements := []struct {
		key      string
		value    string
		priority int
	}{
		{"c", "value-c", 3},
		{"a", "value-a", 1},
		{"b", "value-b", 2},
	}

	// Push elements
	for _, e := range elements {
		hm.Push(e.key, e.value, e.priority)
	}

	// Verify order through Pop (should be reverse of min heap)
	expected := []string{"c", "b", "a"}
	for _, exp := range expected {
		entry, ok := hm.Pop()
		if !ok || entry.Key != exp {
			t.Errorf("Expected key %s, got %s", exp, entry.Key)
		}
	}
}

func TestHeapMapStressTest(t *testing.T) {
	hm := NewHeapMap[int, string, int](func(a, b int) bool { return a < b })

	// Add many elements
	for i := 1000; i >= 0; i-- {
		hm.Push(i, "value", i)
	}

	// Verify they come out in order
	lastPriority := -1
	for i := 0; i <= 1000; i++ {
		entry, ok := hm.Pop()
		if !ok {
			t.Fatal("Failed to pop element")
		}
		if entry.Priority < lastPriority {
			t.Error("Elements not in correct order")
		}
		lastPriority = entry.Priority
	}
}
