package lamport

import "chatsapp/internal/timestamps"

type LamportTime uint32

func (t LamportTime) LessThan(other LamportTime) bool {
	return t < other
}

func (t LamportTime) WithPid(pid timestamps.Pid) timestamps.Timestamp {
	return timestamps.Timestamp{Seqnum: uint32(t), Pid: pid}
}
