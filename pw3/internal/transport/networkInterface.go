package transport

// Represents any structure capable of handling a message received from the network.
type MessageHandler interface {
	HandleNetworkMessage(*Message) (wasHandled bool)
}

// Unique identifier for a handler.
type HandlerId uint32

// Represents a network interface that can send and receive messages.
type NetworkInterface interface {
	// Send a message to the given address.
	Send(addr Address, payload []byte) error
	// Register a handler for incoming messages.
	RegisterHandler(MessageHandler) HandlerId
	// Unregister a handler.
	UnregisterHandler(id HandlerId)
	// Close the network interface.
	Close()
}

// Represents as sent and received by a network interface.
type Message struct {
	/** Source address */
	Source  Address
	Payload []byte
}
