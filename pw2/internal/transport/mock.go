package transport

import (
	"chatsapp/internal/utils"
)

// Mock network interface that can simulate sending and receiving messages.
type MockNetworkInterface struct {
	uidGenerator <-chan HandlerId
	handlers     map[HandlerId]MessageHandler

	SentMessages chan *mockMessage
}

// Creates a new [MockNetworkInterface].
func NewMockNetworkInterface() *MockNetworkInterface {
	return &MockNetworkInterface{
		uidGenerator: genHandlerIds(),
		handlers:     make(map[HandlerId]MessageHandler),
		SentMessages: make(chan *mockMessage, 1),
	}
}

// internal structuro of the [MockNetworkInterface]
type mockMessage struct {
	Message
	Destination Address
}

// Creates a new [mockMessage] with the given source, destination and payload.
func newMockMessage(Source Address, Destination Address, Payload []byte) mockMessage {
	return mockMessage{
		Message: Message{
			Source:  Source,
			Payload: Payload,
		},
		Destination: Destination,
	}
}

// Returns a generator of unique handler IDs
func genHandlerIds() <-chan HandlerId {
	g := utils.NewUIDGenerator()
	ch := make(chan HandlerId)
	go func() {
		for {
			ch <- HandlerId(<-g)
		}
	}()
	return ch
}

func (m *MockNetworkInterface) Send(dest Address, payload []byte) error {
	addr, _ := NewAddress("127.0.0.1:5000")
	msg := newMockMessage(addr, dest, payload)
	m.SentMessages <- &msg
	return nil
}

/** Returns true iff protocol was registered. */
func (m *MockNetworkInterface) RegisterHandler(handler MessageHandler) HandlerId {
	id := <-m.uidGenerator
	m.handlers[id] = handler
	return id
}

func (m *MockNetworkInterface) UnregisterHandler(id HandlerId) {
	_, ok := m.handlers[id]
	if ok {
		delete(m.handlers, id)
	}
}

func (m *MockNetworkInterface) ReceiveFromNetwork(msg *Message) {
	for _, handler := range m.handlers {
		handler.HandleNetworkMessage(msg)
	}
}

func (m *MockNetworkInterface) Close() {
	close(m.SentMessages)
}
