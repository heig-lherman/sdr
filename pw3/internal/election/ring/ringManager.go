package ring

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/server/dispatcher"
	"chatsapp/internal/timestamps"
	"chatsapp/internal/transport"
	goring "container/ring"
	"encoding/gob"
	"fmt"
	"time"
)

type address = transport.Address

type messageType uint8

const (
	payloadType messageType = iota
	ackMsg
)

// defaultTimeout is the default timeout duration for sending requests to the next node in the ring.
const defaultTimeout = 500 * time.Millisecond

type message struct {
	Type      messageType
	Timestamp timestamps.Timestamp
	Payload   dispatcher.Message
}

func (m message) String() string {
	tp := "MSG"
	if m.Type == ackMsg {
		tp = "ACK"
	}
	return fmt.Sprintf("Ring-%s{%s, %s}", tp, m.Timestamp, m.Payload)
}

func (m message) RegisterToGob() {
	gob.Register(m.Timestamp)
	if m.Payload != nil {
		m.Payload.RegisterToGob()
	}
	gob.Register(m)
}

// Interface for a Ring Maintainer.
//
// It garantees that messages sent with [SentToNext] will be received by the next correct node in the ring,
// assuming low network latency. In case of network latencies, some nodes on the ring may be skipped.
type RingMaintainer interface {
	// Send a message to the next correct node in the ring.
	SendToNext(msg dispatcher.Message)
	// Receive a message from the previous correct node in the ring.
	ReceiveFromPrev() dispatcher.Message
}

type incomingMessage struct {
	msg message
	src address
}

type outgoingMessage struct {
	msg  message
	ring goring.Ring
}

// Unexported implementation of the RingMaintainer interface.
type ringMaintainer struct {
	log  *logging.Logger
	disp dispatcher.Dispatcher

	self    address
	ring    []address
	timeout time.Duration

	sendMsg chan dispatcher.Message
	waitMsg chan chan<- dispatcher.Message

	incAck     chan incomingMessage
	incPayload chan incomingMessage
	outMsg     chan outgoingMessage
}

/*
Constructs and returns a new RingMaintainer instance.

Parameters
  - logger: The logger to use for logging messages.
  - disp: The disp to use for sending messages to processes in the network.
  - self: The address of the current process.
  - ring: A slice of addresses, the order of which represents the ring. Note that [self] *must* appear in the slice, though it may appear in any position.
*/
func NewRingMaintainer(
	logger *logging.Logger,
	dispatcher dispatcher.Dispatcher,
	self address,
	ring []address,
) RingMaintainer {
	return newRingMaintainer(logger, dispatcher, self, ring, defaultTimeout)
}

// Creates a new ringMaintainer instance.
func newRingMaintainer(
	logger *logging.Logger,
	disp dispatcher.Dispatcher,
	self address,
	ring []address,
	timeoutDuration time.Duration,
) *ringMaintainer {
	rm := &ringMaintainer{
		log:        logger,
		disp:       disp,
		self:       self,
		ring:       ring,
		timeout:    timeoutDuration,
		sendMsg:    make(chan dispatcher.Message),
		waitMsg:    make(chan chan<- dispatcher.Message),
		incAck:     make(chan incomingMessage),
		incPayload: make(chan incomingMessage),
		outMsg:     make(chan outgoingMessage),
	}

	disp.Register(message{}, rm.handleIncomingMessage)
	go rm.processSends()
	go rm.handleState()

	return rm
}

// SendToNext sends a message to the next correct node in the ring, in a non-blocking manner.
func (rm *ringMaintainer) SendToNext(msg dispatcher.Message) {
	go func() { rm.sendMsg <- msg }()
}

// ReceiveFromPrev blocks until a message is received from the previous correct node in the ring.
// Note: messages are not multiplexed, calling multiple times concurrently to receive messages is undefined behavior.
func (rm *ringMaintainer) ReceiveFromPrev() dispatcher.Message {
	res := make(chan dispatcher.Message)
	rm.waitMsg <- res
	return <-res
}

// handleIncomingMessage is a [dispatcher.ProtocolHandler] that handles incoming ring messages.
func (rm *ringMaintainer) handleIncomingMessage(msg dispatcher.Message, source address) {
	m, ok := msg.(message)
	if !ok {
		rm.log.Warnf("Received unexpected message type %T, ignoring...", msg)
		return
	}

	incMsg := incomingMessage{m, source}
	switch m.Type {
	case payloadType:
		rm.incPayload <- incMsg
	case ackMsg:
		rm.incAck <- incMsg
	}
}

// handleState is the main goroutine that handles the state of the ring maintainer.
func (rm *ringMaintainer) handleState() {
	r := goring.New(len(rm.ring))
	for _, addr := range rm.ring {
		r.Value = addr
		r = r.Next()
	}

	ts := timestamps.NewLamportTimestampHandler(timestamps.Pid(rm.self.String()), 0)

	for {
		select {
		case msg := <-rm.sendMsg:
			rmMsg := message{payloadType, ts.IncrementTimestamp(), msg}
			// We locate the current address so that we will always end the loop at ourselves.
			for r.Value.(address) != rm.self {
				r = r.Next()
			}
			go func() { rm.outMsg <- outgoingMessage{rmMsg, *r.Next()} }()

		case res := <-rm.waitMsg:
			// TODO ask if we should handle messages as they come without necessarily having a call to ReceivePrev
			rm.log.Info("Waiting for message from previous node...")
			go func() {
				msg := <-rm.incPayload
				rm.log.Infof("Received message %#v", msg.msg)
				res <- msg.msg.Payload
				rm.sendMessage(message{ackMsg, msg.msg.Timestamp, nil}, msg.src)
			}()
		}
	}
}

// processSends is the main goroutine that handles sending messages to the next correct node in the ring, it ensures
// that only one message is sent at a time so that we avoid confusing ACKs.
func (rm *ringMaintainer) processSends() {
	for {
		select {
		case msg := <-rm.outMsg:
			// TODO ask should we be able to have concurrent sends with ack identifications using timestamps?
			rm.processSend(msg.msg, msg.ring)
		}
	}
}

// processSend sends a message to the next correct node in the ring, and waits for an ACK in return.
// the ring of address is passed value so that the ring can be updated in case of a timeout, the first value of the ring
// shall be the address of the first node to which a message is sent.
func (rm *ringMaintainer) processSend(msg message, ring goring.Ring) {
	// 1. Send the message to the next node in the ring.
	rm.sendMessage(msg, ring.Value.(address))

	// 2. Start a timeout with the given timeout duration.
	for {
		select {
		// 2.a. The timeout is reached, resend the message to the next node in the ring.
		//      Note: we thus increment the ring at this point for this current execution context.
		case <-time.After(rm.timeout):
			ring = *ring.Next()
			next := ring.Value.(address)
			rm.log.Warnf("[TIMEOUT] Request timed out. Resending to %v...", next)
			rm.sendMessage(msg, ring.Value.(address))

		// 2.b. If an ACK is received, we verify that it is from the expected source, and return.
		case ack := <-rm.incAck:
			if ack.src != ring.Value.(address) || ack.msg.Timestamp != msg.Timestamp {
				rm.log.Warnf("Ignoring ack from unexpected source %v", ack.src)
				continue
			}

			rm.log.Infof("Received ACK from %v", ack.src)
			return
		}
	}
}

// sendsMessage wraps calls to the dispatcher, it ensures the message is not sent to self via the network.
func (rm *ringMaintainer) sendMessage(msg message, addr address) {
	rm.log.Infof("Sending message %#v to %v", msg, addr)
	if addr == rm.self {
		rm.log.Warnf("Sending message to self, %v", msg)
		rm.handleIncomingMessage(msg, rm.self)
	} else {
		rm.disp.Send(msg, addr)
	}
}
