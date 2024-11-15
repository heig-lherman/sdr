package mutex

import (
	"chatsapp/internal/lamport"
	"chatsapp/internal/logging"
	"chatsapp/internal/utils"
	"errors"
)

var ErrMutexInUse = errors.New("mutex is already in use")
var ErrMutexNetworkClosed = errors.New("network channel closed")

type lamportStateMap = utils.HeapMap[Pid, messageType, timestamp]

type OutgoingMessage struct {
	// The destination of the message.
	Destination Pid
	// The message to send.
	Message Message
}

type NetWrapper struct {
	// The channel on which the mutex instance will send messages to the network.
	IntoNet chan<- OutgoingMessage
	// The channel on which the mutex instance will receive messages from the network.
	FromNet <-chan Message
}

// An implementation of the Lamport mutex algorithm, aligning to the Mutex interface.
type lamportMutex struct {
	log     *logging.Logger
	network NetWrapper

	ts        lamport.Clock
	self      Pid
	neighbors []Pid

	requestAccess chan chan error
	releaseAccess chan struct{}
}

/*
NewLamportMutex constructs and returns a new Lamport Mutex.

Parameters:
  - logger: The logger to use for logging messages.
  - network: The network that the mutex will use to communicate with neighbors.
  - self: The process ID of the current process.
  - neighbors: A slice of process IDs representing the neighboring processes.

Returns:
  - Mutex: A Lamport Mutex.
*/
func NewLamportMutex(logger *logging.Logger, networkWrapper NetWrapper, self Pid, neighbors []Pid) Mutex {
	lm := &lamportMutex{
		log:           logger,
		network:       networkWrapper,
		ts:            lamport.NewLamportClock(),
		self:          self,
		neighbors:     neighbors,
		requestAccess: make(chan chan error),
		releaseAccess: make(chan struct{}),
	}

	// Launch the main goroutine
	go lm.handleState()

	return lm
}

func (lm *lamportMutex) Request() (release func(), err error) {
	waitAccess := make(chan error)
	lm.requestAccess <- waitAccess

	err = <-waitAccess
	if err != nil {
		return nil, err
	}

	return func() { lm.releaseAccess <- struct{}{} }, nil
}

/*
 * handleState is the main loop of the Lamport Mutex, handling the state of the mutex
 * and reacting to changes in the network.
 *
 * Inner state:
 *   - requests: A priority queue of requests to enter the critical section.
 *   - receivedAcks: A set of processes that have acknowledged the current process' request.
 *   - currentWaiter: The process waiting to enter the critical section.
 *
 * The loop reacts to three types of events:
 *   - Request access: A process requests access to the critical section.
 *   - Release access: A process releases access to the critical section.
 *   - Incoming message: A message is received from a neighbor, then dispatched according to the message type.
 */
func (lm *lamportMutex) handleState() {
	state := utils.NewHeapMap[Pid, messageType, timestamp](func(a, b timestamp) bool { return a.LessThan(b) })
	state.Push(lm.self, relMsg, timestamp{Pid: lm.self})
	for _, neighbor := range lm.neighbors {
		state.Push(neighbor, relMsg, timestamp{Pid: neighbor})
	}

	var currentWaiter chan error

	lm.log.Info("Lamport Mutex is ready to handle requests")
	for {
		select {
		case <-lm.releaseAccess:
			lm.handleRelease(state)
			continue
		case waitAccess := <-lm.requestAccess:
			if currentWaiter != nil {
				waitAccess <- ErrMutexInUse
				continue
			}

			currentWaiter = waitAccess
			lm.handleNewRequest(state)
		case msg, ok := <-lm.network.FromNet:
			if !ok {
				lm.log.Errorf("Network channel closed")
				if currentWaiter != nil {
					currentWaiter <- ErrMutexNetworkClosed
					currentWaiter = nil
				}

				return
			}

			lm.handleIncomingMessage(state, msg)
		}

		if currentWaiter != nil && lm.checkCriticalSection(state) {
			currentWaiter <- nil
			currentWaiter = nil
		}
	}
}

// handleNewRequest handles a new request for the mutex by the current process.
func (lm *lamportMutex) handleNewRequest(state lamportStateMap) {
	lm.log.Info("Requesting the mutex")
	ts := lm.ts.Increment().WithPid(lm.self)
	msg := Message{Type: reqMsg, TS: ts}
	state.Push(lm.self, reqMsg, ts)
	lm.broadcastToNeighbors(msg)
}

// handleRelease handles the release of the mutex by the current process.
func (lm *lamportMutex) handleRelease(state lamportStateMap) {
	lm.log.Info("Releasing the mutex")
	ts := lm.ts.Increment().WithPid(lm.self)
	msg := Message{Type: relMsg, TS: ts}
	state.Push(lm.self, relMsg, ts)
	lm.broadcastToNeighbors(msg)
}

// handleIncomingMessage handles an incoming message from a neighbor,
// updating the state of the mutex accordingly.
func (lm *lamportMutex) handleIncomingMessage(
	state lamportStateMap,
	msg Message,
) {
	sender := msg.GetSource()
	if sender != lm.self {
		lm.ts.Witness(lamport.LamportTime(msg.TS.Seqnum))
	}

	switch msg.Type {
	case reqMsg:
		lm.log.Infof("Received request message from %v with %v timestamp", sender, msg.TS)
		state.Push(sender, reqMsg, msg.TS)
		lm.network.IntoNet <- OutgoingMessage{
			Destination: sender,
			Message: Message{
				Type: ackMsg,
				TS:   lm.ts.Time().WithPid(lm.self),
			},
		}

	case relMsg:
		lm.log.Infof("Received release message from %v with %v timestamp", sender, msg.TS)
		state.Push(sender, relMsg, msg.TS)

	case ackMsg:
		lm.log.Infof("Received ack message from %v with %v timestamp", sender, msg.TS)
		if r, ok := state.Get(sender); ok && r.Value != reqMsg {
			state.Push(sender, ackMsg, msg.TS)
		}
	}
}

// checkCriticalSection checks if the current process can enter the critical section.
func (lm *lamportMutex) checkCriticalSection(state lamportStateMap) bool {
	top, ok := state.Peek()
	if !ok {
		// reason: no requests in the queue, should in theory never occur since we store every process in state map
		lm.log.Warn("No requests in the queue")
		return false
	}

	if top.Key == lm.self && top.Value == reqMsg {
		lm.log.Info("Process is approved to enter critical section")
		return true
	}

	// reason: another process has a request with a higher priority
	lm.log.Infof("Cannot enter -> process %v has a higher priority %s message", top.Key, top.Value)
	return false
}

// broadcastToNeighbors sends a message to all neighbors.
func (lm *lamportMutex) broadcastToNeighbors(msg Message) {
	lm.log.Infof("Sending %s message with %v timestamp to neighbors", msg.Type, msg.TS)
	for _, neighbor := range lm.neighbors {
		lm.network.IntoNet <- OutgoingMessage{
			Destination: neighbor,
			Message:     msg,
		}
	}
}
