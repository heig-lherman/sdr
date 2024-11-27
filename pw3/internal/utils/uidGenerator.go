package utils

// Generator of unique identifiers.
type UIDGenerator <-chan uint32

// Creates a new [UIDGenerator] that generates unique identifiers. Note that it spawns a goroutine.
func NewUIDGenerator() UIDGenerator {
	c := make(chan uint32)
	go func() {
		var i uint32
		for {
			c <- i
			i++
		}
	}()
	return c
}
