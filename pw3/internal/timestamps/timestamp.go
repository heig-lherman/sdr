package timestamps

import "fmt"

// A process identifier
type Pid string

// A timestamp
type Timestamp struct {
	// The sequence number
	Seqnum uint32
	// The Pid of the process on which the timestamp was generated. Used to break ties in sequence numbers.
	Pid Pid
}

func (ts Timestamp) LessThan(other Timestamp) bool {
	return ts.Seqnum < other.Seqnum || (ts.Seqnum == other.Seqnum && ts.Pid < other.Pid)
}

func (ts Timestamp) String() string {
	return fmt.Sprintf("TS(%s:%v)", ts.Pid, ts.Seqnum)
}
