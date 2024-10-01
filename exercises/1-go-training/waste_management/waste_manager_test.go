package dispatcher

import (
	"fmt"
	"testing"
)

func newTestWasteManager() WasteManager {
	return NewWasteManager(func(obj Scrap) (bool, Waste) {
		isWaste := len(obj) > 4 && obj[:5] == "waste"
		return !isWaste, Waste(obj)
	}, func(obj Scrap) RecycledGood {
		return RecycledGood("recycled goods from " + obj)
	})
}

func TestRecyclable(t *testing.T) {
	m := newTestWasteManager()

	m.Process("scrap1")

	if m.NextRecycledGood() != "recycled goods from scrap1" {
		t.Error("Expected 'recycled goods from scrap1'")
	}
}

func TestWaste(t *testing.T) {
	m := newTestWasteManager()

	m.Process("waste1")

	if m.NextWaste() != "waste1" {
		t.Error("Expected 'waste1'")
	}
}

func TestGoodThenWaste(t *testing.T) {
	m := newTestWasteManager()

	m.Process("good1")
	m.Process("waste1")

	if g := m.NextRecycledGood(); g != "recycled goods from good1" {
		t.Errorf("Expected 'recycled goods from good1'; got '%s'", g)
	}
	if w := m.NextWaste(); w != "waste1" {
		t.Errorf("Expected 'waste1'; got '%s'", w)
	}
}

func TestWasteThenGood(t *testing.T) {
	w := newTestWasteManager()

	w.Process("waste1")
	w.Process("good1")

	if w := w.NextWaste(); w != "waste1" {
		t.Errorf("Expected 'waste1'; got '%s'", w)
	}
	if g := w.NextRecycledGood(); g != "recycled goods from good1" {
		t.Errorf("Expected 'recycled goods from good1'; got '%s'", g)
	}
}

func TestOrder(t *testing.T) {
	d := newTestWasteManager()

	scraps := []Scrap{}

	for i := 0; i < 100; i++ {
		scraps = append(scraps, Scrap(fmt.Sprintf("good %d", i)))
		d.Process(scraps[i])
	}

	for i := 0; i < 100; i++ {
		expected := RecycledGood(fmt.Sprintf("recycled goods from good %d", i))
		actual := d.NextRecycledGood()
		if actual != expected {
			t.Errorf("Expected '%s'; got '%s'", expected, actual)
		}
	}
}

func TestBuffering(t *testing.T) {
	d := newTestWasteManager()

	numMessages := 100_000

	for i := 0; i < numMessages; i++ {
		d.Process("good")
	}

	for i := 0; i < numMessages; i++ {
		expected := RecycledGood("recycled goods from good")
		actual := d.NextRecycledGood()
		if actual != expected {
			t.Errorf("Expected '%s'; got '%s'", expected, actual)
		}
	}
}
