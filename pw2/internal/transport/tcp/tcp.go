package tcp

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"encoding/gob"
)

type Address = transport.Address
type Message = transport.Message
type HandlerId = transport.HandlerId
type MessageHandler = transport.MessageHandler
type NetworkInterface = transport.NetworkInterface

// Payload associated to the address to which it is destined.
type sendRequest struct {
	addr    Address
	payload []byte
	err     chan<- error
}

// Internal representation of a handler registration.
type registration struct {
	id      HandlerId
	handler MessageHandler
}

// Implements the [NetworkInterface] interface for TCP connections.
type TCP struct {
	uidGenerator utils.UIDGenerator
	logger       *logging.Logger

	local Address

	remotes *remotesHandler

	sendRequests     chan sendRequest
	receivedMessages chan Message
	registrations    chan registration
	unregistrations  chan HandlerId

	closeChan chan struct{}
}

// Constructs and returns a new [TCP] instance capable of sending messages to a fixed set of neighbors.
//   - self: The address of the local node.
//   - neighbors: The addresses of the neighbors of the local node, itself excluded.
//   - logger: The logger to use for logging messages.
func NewTCP(local Address, log *logging.Logger) NetworkInterface {
	gob.Register(Message{})

	receivedRequests := make(chan Message)

	tcp := TCP{
		local:        local,
		logger:       log,
		uidGenerator: utils.NewUIDGenerator(),

		remotes: newRemoteHandler(local, log.WithPostfix("remotes"), receivedRequests),

		sendRequests:     make(chan sendRequest),
		receivedMessages: receivedRequests,

		registrations:   make(chan registration),
		unregistrations: make(chan HandlerId),

		closeChan: make(chan struct{}),
	}

	go tcp.handleSendRequests()
	go tcp.handleReceivedMessages()

	return &tcp
}

// Main goroutine for handling received messages.
//
// Because handlers can register and unregister dynamically during execution, it must be handled by a single goroutine to avoid concurrent access.
//
// This goroutine manages these handlers, (un)registration requests, and dispatching received messages to the registered handlers.
func (tcp *TCP) handleReceivedMessages() {
	registeredHandlers := make(map[HandlerId]MessageHandler)

	for {
		select {
		case msg := <-tcp.receivedMessages:
			tcp.logger.Info("TCP received message from", msg.Source, " through RR. Dispatching it among ", len(registeredHandlers), " handlers.")
			tcp.handleReceivedMessage(msg, registeredHandlers)
		case reg := <-tcp.registrations:
			registeredHandlers[reg.id] = reg.handler
		case id := <-tcp.unregistrations:
			delete(registeredHandlers, id)
		case <-tcp.closeChan:
			tcp.logger.Warn("TCP's received-messages handler is closing.")
			return
		}
	}
}

// Dispatches a received message to the registered handlers.
func (tcp *TCP) handleReceivedMessage(msg Message, handlers map[HandlerId]MessageHandler) {
	for _, handler := range handlers {
		if handler.HandleNetworkMessage(&msg) {
			break
		}
	}
}

// Main goroutine for sending requests.
//
// This goroutine listens for requests to send messages and forwards them to the appropriate remote.
func (tcp *TCP) handleSendRequests() {
	for {
		select {
		case req := <-tcp.sendRequests:
			tcp.logger.Info("TCP received request to send message to ", req.addr)
			tcp.remotes.SendToRemote(req)
		case <-tcp.closeChan:
			tcp.logger.Warn("TCP's state handler is closing.")
			tcp.remotes.Close()
			return
		}
	}
}

func (tcp *TCP) Send(dest Address, payload []byte) error {
	errChan := make(chan error)
	tcp.sendRequests <- sendRequest{addr: dest, payload: payload, err: errChan}

	return <-errChan
}

func (tcp *TCP) RegisterHandler(handler MessageHandler) HandlerId {
	nextUid := HandlerId(<-tcp.uidGenerator)
	tcp.registrations <- registration{
		id:      nextUid,
		handler: handler,
	}
	return nextUid
}

func (tcp *TCP) UnregisterHandler(id HandlerId) {
	tcp.unregistrations <- id
}

func (tcp *TCP) Close() {
	tcp.logger.Warnf("Closing TCP")
	close(tcp.closeChan)
}
