package pulsing

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/messages"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"chatsapp/internal/utils/option"
	"chatsapp/internal/utils/result"
	"log"
)

// PulseHandler is the type of functions that can handle a received pulse and return the pulse to be sent further.
type PulseHandler[M any] func(id PulseID, received M, from transport.Address) (propagated M, err error)

// EchoHandler is the type of functions that can handle received echoes and aggregate them into the one that will be propagated.
type EchoHandler[M any] func(id PulseID, pulse M, echoes []SourcedMessage[M]) (propagated M, err error)

// SentMessage is a [PulsarMessage] that a [Pulsar] instance wants to send over the network to a given destination address.
type SentMessage[M any] messages.Destined[PulsarMessage[M]]

// NetSender is the type of channels on which a [Pulsar] instance sends messages intended to be over the network.
type NetSender[M any] chan<- SentMessage[M]

// ReceivedMessage is a [PulsarMessage] that a [Pulsar] instance has received from the network, along with the address of the sender.
type ReceivedMessage[M any] messages.Sourced[PulsarMessage[M]]

// NetReceiver is the type of channels on which a [Pulsar] instance receives messages from the network.
type NetReceiver[M any] <-chan ReceivedMessage[M]

// Pulsar is the interface for a pulsar instance.
type Pulsar[M any] interface {
	StartPulse(pulse M) (echo M, err error)
}

type newPulse[M any] struct {
	msg  M
	echo chan<- result.ErrorOr[M]
}

type pulsar[M any] struct {
	log *logging.Logger
	uid utils.UIDGenerator

	self      transport.Address
	neighbors []transport.Address

	pulseHandler PulseHandler[M]
	echoHandler  EchoHandler[M]
	pulsarToNet  NetSender[M]
	netToPulsar  NetReceiver[M]

	pulseRequests chan<- newPulse[M]
}

// NewPulsar creates a new pulsar instance.
func NewPulsar[M any](
	logger *logging.Logger,
	self transport.Address,
	pulseHandler PulseHandler[M],
	echoHandler EchoHandler[M],
	pulsarToNet NetSender[M],
	netToPulsar NetReceiver[M],
	neighbors []transport.Address,
) Pulsar[M] {
	PulsarMessage[M]{}.RegisterToGob()
	logger.Infof("Initializing pulsar at %v with %d neighbors using %T", self, len(neighbors), PulsarMessage[M]{})

	requests := make(chan newPulse[M])
	p := &pulsar[M]{
		log:           logger,
		uid:           utils.NewUIDGenerator(),
		self:          self,
		neighbors:     neighbors,
		pulseHandler:  pulseHandler,
		echoHandler:   echoHandler,
		pulsarToNet:   pulsarToNet,
		netToPulsar:   netToPulsar,
		pulseRequests: requests,
	}

	go p.handlePulses(requests)
	logger.Infof("Pulsar started successfully at %v", self)

	return p
}

func (p *pulsar[M]) StartPulse(pulse M) (M, error) {
	p.log.Infof("Starting new pulse from %v", p.self)
	echo := make(chan result.ErrorOr[M])
	p.pulseRequests <- newPulse[M]{msg: pulse, echo: echo}
	return (<-echo).Unwrap()
}

func (p *pulsar[M]) handlePulses(
	pulseRequests <-chan newPulse[M],
) {
	p.log.Infof("Starting pulse handler routine")
	states := make(pulsarStates[M])
	for {
		select {
		case req := <-pulseRequests:
			p.handlePulseRequest(states, req)
		case msg := <-p.netToPulsar:
			switch msg.Message.Type {
			case Pulse:
				p.log.Infof("Received pulse message for pulse %v from %v", msg.Message.ID, msg.From)
				p.handleIncomingPulse(states, msg.From, msg.Message)
			case Echo:
				p.log.Infof("Received echo message for pulse %v from %v", msg.Message.ID, msg.From)
				p.handleIncomingEcho(states, msg.From, msg.Message)
			}
		}
	}
}

func (p *pulsar[M]) handlePulseRequest(
	states pulsarStates[M],
	req newPulse[M],
) {
	// 1. Generate a new pulse ID
	pid := NewPulseID(p.self, <-p.uid)
	p.log.Infof("Initiating new pulse with ID %v", pid)

	// 2. Define our parent as nil as we are the source
	states.initSourceState(pid, req, len(p.neighbors))

	// 3. Send the pulse to all neighbors
	msg := NewPulseMessage[M](req.msg, pid)
	p.log.Infof("Broadcasting pulse %v to %d neighbors", pid, len(p.neighbors))
	for _, neighbor := range p.neighbors {
		p.pulsarToNet <- SentMessage[M]{Message: msg, To: neighbor}
	}

	// 4. Verify received echoes in case we have no neighbors
	p.verifyReceivedEchoes(states, pid)
}

func (p *pulsar[M]) handleIncomingPulse(
	states pulsarStates[M],
	parent transport.Address,
	msg PulsarMessage[M],
) {
	if msg.Type != Pulse {
		p.log.Errorf("Protocol violation: received non-pulse message as pulse from %v", parent)
		panic("Received non-pulse message as pulse")
	}

	// 1. If we know this state, decrement the awaited echoes. It means there is a loop in the network.
	if state, ok := states[msg.ID]; ok {
		p.log.Infof("Detected network loop for pulse %v from %v, decrementing awaited echoes", msg.ID, parent)
		state.awaitedEchoes--
	} else {
		// 2. If we do not know this state, create a new one
		p.log.Infof("Processing new pulse %v from %v", msg.ID, parent)
		state = states.initEchoState(msg, parent, len(p.neighbors))

		// 3. Invoke the pulse handler
		res := result.Wrap(p.pulseHandler(msg.ID, msg.Payload, parent))
		if res.IsError() {
			p.log.Errorf("Pulse handler failed for %v: %v", msg.ID, res.GetError())
		}

		// Generate the pulse message from the handler result, panics if error
		pulse := NewPulseMessage[M](res.GetResult(), msg.ID)

		// 4. Broadcast the pulse to all neighbors except the origin
		for _, n := range p.neighbors {
			if n == parent {
				continue
			}

			p.pulsarToNet <- SentMessage[M]{Message: pulse, To: n}
		}

		p.log.Infof("Forwarded pulse %v to %d neighbors", msg.ID, len(p.neighbors)-1)
	}

	// 5. Verify received echoes
	p.verifyReceivedEchoes(states, msg.ID)
}

func (p *pulsar[M]) handleIncomingEcho(
	states pulsarStates[M],
	neighbor transport.Address,
	msg PulsarMessage[M],
) {
	if msg.Type != Echo {
		p.log.Errorf("Protocol violation: received non-echo message as echo from %v", neighbor)
		panic("Received non-echo message as echo")
	}

	// 1. Get the state for the pulse
	p.log.Infof("Processing echo for pulse %v from %v", msg.ID, neighbor)
	state, ok := states[msg.ID]
	if !ok {
		p.log.Errorf("Received echo for unknown pulse %v from %v", msg.ID, neighbor)
		return
	}

	// 2. Add the echo to the state
	state.receivedEchoes = append(state.receivedEchoes, SourcedMessage[M]{Msg: msg.Payload, From: neighbor})
	p.log.Infof(
		"Added echo from %v for pulse %v, total echoes: %d/%d",
		neighbor, msg.ID, len(state.receivedEchoes), state.awaitedEchoes,
	)

	// 3. Verify received echoes
	p.verifyReceivedEchoes(states, msg.ID)
}

func (p *pulsar[M]) verifyReceivedEchoes(
	states pulsarStates[M],
	pid PulseID,
) {
	state, ok := states[pid]
	if !ok {
		log.Fatalf("Tried to verify echoes for unknown pulse %v", pid)
	}

	// 1. If we have not received all echoes, return early
	if len(state.receivedEchoes) != state.awaitedEchoes {
		// We are still awaiting some echoes
		p.log.Infof("Awaiting %v echoes for %v, got %v", state.awaitedEchoes, pid, len(state.receivedEchoes))
		return
	}

	p.log.Infof("Received all %v echoes for %v", state.awaitedEchoes, pid)

	// 2. When we have all echoes, call the echo handler
	res := result.Wrap(p.echoHandler(pid, state.pulse, state.receivedEchoes))

	// 3. If we are the source, return the result to the caller, otherwise send the result to our parent
	if state.parent.IsNone() {
		// We are the source, return the result to the caller
		p.log.Infof("Pulse %v completed, result: %v", pid, res)
		states.finishPulse(pid, res)
	} else {
		p.log.Infof("Pulse %v completed, forwarding result to %v", pid, state.parent.Get())
		// Propagate the result to the parent, panics if an error was received
		echo := NewEchoMessage[M](res.GetResult(), pid)
		p.pulsarToNet <- SentMessage[M]{Message: echo, To: state.parent.Get()}
	}
}

// pulsarStates is the inner state of a pulsar instance, not thread-safe.
type pulsarStates[M any] map[PulseID]*pulseState[M]

// pulseState is the state of a pulsar instance for a given pulse ID.
type pulseState[M any] struct {
	pulse  M
	parent option.Option[transport.Address]

	receivedEchoes []SourcedMessage[M]
	awaitedEchoes  int

	resultWaiter chan<- result.ErrorOr[M]
}

func (s pulsarStates[M]) initSourceState(
	pid PulseID,
	req newPulse[M],
	neighborCount int,
) *pulseState[M] {
	if _, ok := s[pid]; ok {
		log.Fatalf("State for the source %v already initialized", pid)
	}

	s[pid] = &pulseState[M]{
		pulse:          req.msg,
		parent:         option.None[transport.Address](),
		receivedEchoes: make([]SourcedMessage[M], 0, neighborCount),
		awaitedEchoes:  neighborCount,
		resultWaiter:   req.echo,
	}

	return s[pid]
}

func (s pulsarStates[M]) initEchoState(
	pulse PulsarMessage[M],
	parent transport.Address,
	neighborCount int,
) *pulseState[M] {
	if _, ok := s[pulse.ID]; ok {
		log.Fatalf("State for pulse %v already initialized", pulse.ID)
	}

	s[pulse.ID] = &pulseState[M]{
		pulse:          pulse.Payload,
		parent:         option.Some(parent),
		receivedEchoes: make([]SourcedMessage[M], 0, neighborCount-1),
		awaitedEchoes:  neighborCount - 1,
		resultWaiter:   nil,
	}

	return s[pulse.ID]
}

func (s pulsarStates[M]) finishPulse(
	pid PulseID,
	res result.ErrorOr[M],
) {
	if state, ok := s[pid]; !ok {
		log.Fatalf("Tried to finish unknown pulse %v", pid)
	} else if state.resultWaiter == nil {
		log.Fatalf("Tried to finish pulse %v that is not a source", pid)
	} else {
		state.resultWaiter <- res
		delete(s, pid)
	}
}
