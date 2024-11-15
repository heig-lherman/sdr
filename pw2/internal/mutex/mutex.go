package mutex

import "chatsapp/internal/timestamps"

type Pid = timestamps.Pid
type timestamp = timestamps.Timestamp

/*
Interface for a mutex that can be acquired and released.
*/
type Mutex interface {
	/*
		Request permission to enter critical section.

		Returns a channel that should be closed by the critical section when it is done, to signal that the mutex can be released.

		It is recommended to defer the closing of the channel immediately after acquiring the mutex to avoid deadlocks.
	*/
	Request() (release func(), err error)
}
