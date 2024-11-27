package dispatcher

import (
	"bytes"
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"encoding/gob"
	"reflect"
)

// Generic interface for messages that can be sent and received through the dispatcher
type Message interface {
	RegisterToGob()
}

// A function capable of handling a dispatched message
type ProtocolHandler func(msg Message, source transport.Address)

type addressedMessage struct {
	Message
	addr transport.Address
}

type protocolRegistration struct {
	msgType reflect.Type
	handler ProtocolHandler
}

// A dispatcher is responsible for routing messages to the appropriate handlers
type Dispatcher interface {
	// Register a handler for a given message type
	//   - msg: An istance of the message type to register
	//   - handler: The handler to call when a message of this type is received
	Register(msg Message, handler ProtocolHandler)
	// Send a message to a given destination. Will block until the message is guaranteed to have been received by the destination
	Send(msg Message, dest transport.Address)
	// Close the dispatcher, cleaning up resources
	Close()
}

// Local implementation of the Dispatcher interface. Hiding the implementation behind an interface allows for easier testing of modules using the dispatcher.
type dispatcherImpl struct {
	logger *logging.Logger

	selfAddr transport.Address

	network transport.NetworkInterface

	receivedMessages chan addressedMessage
	registrations    chan protocolRegistration

	closeChan chan struct{}
}

/*
Constructs a new dispatcher instance
  - logger: The logger to use for logging messages
  - selfAddr: The address of this process
  - rrs: RR instances to use for communication with other processes
*/
func NewDispatcher(logger *logging.Logger, selfAddr transport.Address, network transport.NetworkInterface) Dispatcher {
	d := dispatcherImpl{
		logger:   logger,
		selfAddr: selfAddr,

		network: network,

		receivedMessages: make(chan addressedMessage, 100),
		registrations:    make(chan protocolRegistration),

		closeChan: make(chan struct{}),
	}

	network.RegisterHandler(&d)

	go d.dispatch()

	return &d
}

func (d *dispatcherImpl) HandleNetworkMessage(msg *transport.Message) (wasHandled bool) {
	wasHandled = true

	decodedMsg := d.decodeMessage(msg.Payload)
	d.receivedMessages <- addressedMessage{decodedMsg, msg.Source}

	return true
}

func (d *dispatcherImpl) Close() {
	close(d.closeChan)
}

// Reports whether the dispatcher is closed
func (d *dispatcherImpl) isClosed() bool {
	select {
	case <-d.closeChan:
		return true
	default:
		return false
	}
}

/*
Main goroutine that maintains the handlers and dispatches messages to them.

Because handlers can be registered dynamically during the execution, they represent dynamic state that must be maintained in a thread-safe way. In order to achieve this, they are handled by a single goroutine, which is this one.

This goroutine handles registration of handlers, and dispatching of messages to the appropriate handlers.
*/
func (d *dispatcherImpl) dispatch() {
	handlers := make(map[reflect.Type]ProtocolHandler)
	for {
		select {
		case reg, ok := <-d.registrations:
			if !ok {
				return
			}
			if _, ok := handlers[reg.msgType]; ok {
				d.logger.Warn("Handler already registered for message type. Overwriting it...", reg.msgType)
			}
			handlers[reg.msgType] = reg.handler
		case msg, ok := <-d.receivedMessages:
			if !ok {
				return
			}
			d.logger.Info("Dispatching message", msg)
			handler, exists := handlers[reflect.TypeOf(msg.Message)]
			if !exists {
				d.logger.Error("No handler for message of type", reflect.TypeOf(msg.Message))
				continue
			}

			handler(msg.Message, msg.addr)
		case <-d.closeChan:
			return
		}
	}
}

func (d *dispatcherImpl) Register(msg Message, handler ProtocolHandler) {
	msg.RegisterToGob()

	if d.isClosed() {
		d.logger.Warn("Dispatcher is closed, not registering handler")
		return
	}

	d.registrations <- protocolRegistration{
		msgType: reflect.TypeOf(msg),
		handler: handler,
	}
}

// Sends and waits for the remote to acknowledge having received the answer
func (d *dispatcherImpl) Send(msg Message, dest transport.Address) {
	d.logger.Infof("Sending message %v to %v", msg, dest)

	encodedMsg := d.encodeMessage(msg)

	err := d.network.Send(dest, encodedMsg)

	if err != nil {
		d.logger.Error("Error sending message")
		return
	}
}

// Encodes a message to a byte slice
func (d *dispatcherImpl) encodeMessage(msg Message) []byte {
	encodedMsg := bytes.Buffer{}
	encoder := gob.NewEncoder(&encodedMsg)
	err := encoder.Encode(&msg)
	if err != nil {
		panic(err)
	}

	return encodedMsg.Bytes()
}

// Decodes a message from a byte slice
func (d *dispatcherImpl) decodeMessage(encodedMsg []byte) Message {
	var decodedMsg Message
	decoder := gob.NewDecoder(bytes.NewReader(encodedMsg))
	err := decoder.Decode(&decodedMsg)
	if err != nil {
		panic(err)
	}

	return decodedMsg
}
