package tcp

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"encoding/gob"
	"errors"
)

type Address = transport.Address
type Message = transport.Message
type MessageHandler = transport.MessageHandler
type HandlerId = transport.HandlerId

// TCP implements the [NetworkInterface] interface for TCP connections.
type TCP struct {
	logger       *logging.Logger
	uidGenerator utils.UIDGenerator
	local        Address

	sendRequests chan destinedMessage

	receivedMessages chan Message

	handlerRegistrations   chan handlerRegistration
	handlerUnregistrations chan HandlerId

	getConnections            chan getConnection
	connectionRegistrations   chan connectionRegistration
	connectionUnregistrations chan Address

	closeChan          chan struct{}
	connCloseConfirm   chan struct{}
	listenCloseConfirm chan struct{}
}

// NewTCP constructs and returns a new [TCP] instance capable of sending messages to a fixed set of neighbors.
//   - self: The address of the local node.
//   - neighbors: The addresses of the neighbors of the local node, itself excluded.
//   - logger: The logger to use for logging messages.
func NewTCP(
	self Address,
	neighbors []Address,
	logger *logging.Logger,
) transport.NetworkInterface {
	gob.Register(Message{})

	tcp := TCP{
		logger:                    logger,
		uidGenerator:              utils.NewUIDGenerator(),
		local:                     self,
		sendRequests:              make(chan destinedMessage),
		receivedMessages:          make(chan Message),
		handlerRegistrations:      make(chan handlerRegistration),
		handlerUnregistrations:    make(chan HandlerId),
		getConnections:            make(chan getConnection),
		connectionRegistrations:   make(chan connectionRegistration),
		connectionUnregistrations: make(chan Address),
		closeChan:                 make(chan struct{}),
		connCloseConfirm:          make(chan struct{}),
		listenCloseConfirm:        make(chan struct{}),
	}

	// start listening for incoming connections
	go tcp.listenIncomingConnections()

	// connect to neighbors and wait for them to connect
	done := make(chan struct{})
	go tcp.connectNeighbors(neighbors, done)

	// state handlers
	go tcp.handleHandlers()
	go tcp.handleConnections()

	// handle send requests once we know the connection pool is ready
	go func() {
		<-done
		tcp.handleSendRequests()
	}()

	return &tcp
}

func (tcp *TCP) IsClosed() bool {
	select {
	case <-tcp.closeChan:
		return true
	default:
		return false
	}
}

func (tcp *TCP) Send(addr transport.Address, payload []byte) error {
	errChannel := make(chan error)
	tcp.sendRequests <- destinedMessage{addr, payload, errChannel}
	select {
	case err := <-errChannel:
		return err
	case <-tcp.closeChan:
		return errors.New("TCP instance closed while sending message")
	}
}

func (tcp *TCP) RegisterHandler(handler transport.MessageHandler) transport.HandlerId {
	nextUid := HandlerId(<-tcp.uidGenerator)
	tcp.handlerRegistrations <- handlerRegistration{nextUid, handler}
	return nextUid
}

func (tcp *TCP) UnregisterHandler(id transport.HandlerId) {
	tcp.handlerUnregistrations <- id
}

func (tcp *TCP) Close() {
	tcp.logger.Warn("Closing TCP instance")
	close(tcp.closeChan)
	<-tcp.connCloseConfirm
	<-tcp.listenCloseConfirm
}
