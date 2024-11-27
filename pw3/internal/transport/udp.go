package transport

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/utils"
	"encoding/gob"
	"net"
)

// Payload associated to the address to which it is destined.
type destinedMessage struct {
	addr    Address
	payload []byte
}

// Internal representation of a handler registration.
type registration struct {
	id      HandlerId
	handler MessageHandler
}

// Implements the [NetworkInterface] interface for UDP connections.
type UDP struct {
	logger       *logging.Logger
	uidGenerator utils.UIDGenerator

	local Address

	sendRequests     chan destinedMessage
	receivedMessages chan Message
	registrations    chan registration
	unregistrations  chan HandlerId

	closeChan chan struct{}
}

// Creates a new [UDP] instance with the given local address and logger.
func NewUDP(local Address, log *logging.Logger) NetworkInterface {
	gob.Register(Message{})
	udp := UDP{
		logger:       log,
		uidGenerator: utils.NewUIDGenerator(),
		local:        local,

		sendRequests:     make(chan destinedMessage),
		receivedMessages: make(chan Message),
		registrations:    make(chan registration),
		unregistrations:  make(chan HandlerId),

		closeChan: make(chan struct{}),
	}

	go udp.listenIncomingMessages()
	go udp.handleState()

	return &udp
}

/*
Main goroutine for handling the state of the UDP connection.

The state is anything that may change dynamically, i.e. the set of registered handlers, and the set of send-channels for each known remote. In order to prevent concurrent access to this state, it must be handled by a single goroutine and all accesses or modifications must be done as instructions to this goroutine, passed through channels.

It handles the following events:
  - On send requests made through [Send], forwards it to that remote's send-channel.
  - On handler (un)registration, updates the set of registered handlers.
  - On messages received from the network, dispatches it to all registered handlers.
  - On close, closes all send-channels and returns.
*/
func (udp *UDP) handleState() {
	registeredHandlers := make(map[HandlerId]MessageHandler)
	sendChans := make(map[Address]chan []byte)

	for {
		select {
		case msg := <-udp.sendRequests:
			udp.handleSend(msg, sendChans)
		case msg := <-udp.receivedMessages:
			udp.logger.Info("UDP received message from", msg.Source, ". Dispatching it among ", len(registeredHandlers), " handlers.")
			for _, handler := range registeredHandlers {
				if handler.HandleNetworkMessage(&msg) {
					break
				}
			}
		case registration := <-udp.registrations:
			udp.logger.Info("UDP registering handler", registration.id)
			registeredHandlers[registration.id] = registration.handler
		case id := <-udp.unregistrations:
			udp.logger.Info("UDP unregistering handler", id)
			delete(registeredHandlers, id)
		case <-udp.closeChan:
			udp.logger.Warn("UDP's state-handler is closing.")

			for _, sendChan := range sendChans {
				close(sendChan)
			}

			return
		}
	}
}

// Handles a send request by pushing the payload to the appropriate send-channel. If none exist, it means that the remote is not yet known; it thus creates a new send-channel for that remote and starts handling it using [startHandlingSends].
func (udp *UDP) handleSend(msg destinedMessage, sendChans map[Address]chan []byte) {
	if _, exists := sendChans[msg.addr]; !exists {
		sendChans[msg.addr] = make(chan []byte)
		udp.startHandlingSends(msg.addr, sendChans[msg.addr])
	}

	sendChans[msg.addr] <- msg.payload
}

// Launches a goroutine that handles the send-channel for the given remote. It forwards any message received on that channel to the remote connection.
func (udp *UDP) startHandlingSends(dest Address, sendRequests chan []byte) {
	udp.logger.Info("Starting to handle sends to", dest)

	go func() {
		conn, err := net.Dial("udp", dest.String())
		if err != nil {
			udp.logger.Error("Error dialing connection in transport:", err)
		}
		defer conn.Close()

		for payload := range sendRequests {
			encoder := gob.NewEncoder(conn)
			udp.logger.Info("UDP starts sending message to", dest)
			message := Message{udp.local, payload}
			err = encoder.Encode(message)
			udp.logger.Info("UDP done sending message to", dest)
			if err != nil {
				udp.logger.Error("Error encoding message in transport:", err)
			}
		}

		udp.logger.Warn("UDP's send-request-handler closed due to closed chan")
	}()
}

func (udp *UDP) Send(dest Address, payload []byte) error {
	udp.sendRequests <- destinedMessage{
		addr:    dest,
		payload: payload,
	}
	return nil
}

func (udp *UDP) RegisterHandler(handler MessageHandler) HandlerId {
	nextUid := HandlerId(<-udp.uidGenerator)

	udp.registrations <- registration{
		id:      nextUid,
		handler: handler,
	}

	return nextUid
}

func (udp *UDP) UnregisterHandler(id HandlerId) {
	udp.unregistrations <- id
}

// Main goroutine for handling incoming messages. Messages received from the network are decoded and forwarded to the main goroutine that handles state.
func (udp *UDP) listenIncomingMessages() error {
	udpAddr, err := net.ResolveUDPAddr("udp", udp.local.String())
	if err != nil {
		return err
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	defer udpConn.Close()

	udp.logger.Info("Listening for messages on ", udp.local)

	receivedChan := make(chan Message)
	go func() {
		for {
			var receivedMessage Message
			decoder := gob.NewDecoder(udpConn)
			err := decoder.Decode(&receivedMessage)
			if err != nil {
				select {
				case <-udp.closeChan:
					udp.logger.Warn("UDP's receive-handler closed due to closed chan")
					return
				default:
					udp.logger.Error("Error decoding message in transport:", err)
					continue
				}
			}
			receivedChan <- receivedMessage
		}
	}()

	for {
		select {
		case <-udp.closeChan:
			udp.logger.Warn("UDP's receive-handler is closing.")
			return nil
		case receivedMessage := <-receivedChan:
			udp.receivedMessages <- receivedMessage
		}
	}
}

func (udp *UDP) Close() {
	close(udp.closeChan)
}
