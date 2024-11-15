package lamport

// Clock is an interface for Lamport logical clocks
type Clock interface {
	// Time is a getter for the current Lamport time value
	Time() LamportTime
	// Increment increments the Lamport time,
	// returning the new value if successful
	Increment() LamportTime
	// Witness is called to update the local time if necessary
	// after witnessing a clock value from another source
	Witness(time LamportTime)
}
