package lamport

import (
	"sync"
	"testing"
)

func TestNewLamportClock(t *testing.T) {
	clock := NewLamportClock()

	if time := clock.Time(); time != 0 {
		t.Errorf("New clock should initialize with time 0, got %d", time)
	}
}

func TestIncrement(t *testing.T) {
	clock := NewLamportClock()

	// Test single increment
	time := clock.Increment()
	if time != 1 {
		t.Errorf("Expected time 1 after first increment, got %d", time)
	}

	// Test multiple increments
	for i := 0; i < 100; i++ {
		before := clock.Time()
		time := clock.Increment()
		if time != before+1 {
			t.Errorf("Expected time %d after increment, got %d", before+1, time)
		}
	}
}

func TestWitness(t *testing.T) {
	tests := []struct {
		name          string
		currentTime   uint32
		witnessTime   LamportTime
		expectedAfter LamportTime
	}{
		{
			name:          "witness_smaller_time",
			currentTime:   5,
			witnessTime:   3,
			expectedAfter: 6,
		},
		{
			name:          "witness_larger_time",
			currentTime:   5,
			witnessTime:   10,
			expectedAfter: 11,
		},
		{
			name:          "witness_equal_time",
			currentTime:   5,
			witnessTime:   5,
			expectedAfter: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &LamportClock{time: tt.currentTime}

			clock.Witness(tt.witnessTime)
			if after := clock.Time(); after != tt.expectedAfter {
				t.Errorf("Expected time %d after witness, got %d", tt.expectedAfter, after)
			}
		})
	}
}

func TestConcurrentOperations(t *testing.T) {
	clock := NewLamportClock()
	const numGoroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // For both increment and witness tests

	// Test concurrent increments
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				_ = clock.Increment()
			}
		}()
	}

	// Test concurrent witness operations
	for i := 0; i < numGoroutines; i++ {
		go func(routine int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				witnessTime := LamportTime(routine*opsPerGoroutine + j)
				clock.Witness(witnessTime)
			}
		}(i)
	}

	wg.Wait()

	// Verify that the final time is at least as large as the number of operations
	finalTime := clock.Time()
	minExpectedTime := LamportTime(numGoroutines * opsPerGoroutine)
	if finalTime < minExpectedTime {
		t.Errorf("Expected final time to be at least %d, got %d", minExpectedTime, finalTime)
	}
}

func TestMonotonicity(t *testing.T) {
	clock := NewLamportClock()

	// Test that time never decreases through mixed operations
	operations := 1000
	var lastTime LamportTime

	for i := 0; i < operations; i++ {
		var currentTime LamportTime

		if i%2 == 0 {
			currentTime = clock.Increment()
		} else {
			clock.Witness(LamportTime(i))
			currentTime = clock.Time()
		}

		if currentTime <= lastTime {
			t.Errorf("Time decreased or stayed same: previous=%d, current=%d", lastTime, currentTime)
		}
		lastTime = currentTime
	}
}

func TestClockBehavior(t *testing.T) {
	// Simulate typical distributed system scenario
	clockA := NewLamportClock()
	clockB := NewLamportClock()

	// Simulate message exchange between two processes
	_ = clockA.Increment()
	timeA2 := clockA.Increment()

	// B receives message from A
	clockB.Witness(timeA2)
	timeB := clockB.Time()
	if timeB <= timeA2 {
		t.Errorf("Clock B should be greater than received time: got B=%d, A=%d", timeB, timeA2)
	}

	// A receives response from B
	clockA.Witness(timeB)
	finalTimeA := clockA.Time()
	if finalTimeA <= timeB {
		t.Errorf("Clock A should be greater than received time: got A=%d, B=%d", finalTimeA, timeB)
	}
}
