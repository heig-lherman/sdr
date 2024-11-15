package lamport

import "sync/atomic"

// LamportClock is a simple implementation of the Lamport logical Clock interface where the time is stored in memory
type LamportClock struct {
	time uint32
}

// NewLamportClock creates a new Clock instance with the timestamp saved in memory
func NewLamportClock() Clock {
	return &LamportClock{time: 0}
}

func (c *LamportClock) Time() LamportTime {
	return LamportTime(atomic.LoadUint32(&c.time))
}

func (c *LamportClock) Increment() LamportTime {
	return LamportTime(atomic.AddUint32(&c.time, 1))
}

func (c *LamportClock) Witness(time LamportTime) {
	for {
		current := atomic.LoadUint32(&c.time)
		other := uint32(time)

		newTime := max(current, other) + 1
		if atomic.CompareAndSwapUint32(&c.time, current, newTime) {
			return
		}
	}
}
