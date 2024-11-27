package mocks

import (
	"chatsapp/internal/server/dispatcher"
	"chatsapp/internal/transport"
	"reflect"
	"testing"
	"time"
)

type dmsg = dispatcher.Message
type address = transport.Address

type addressedMsg struct {
	Payload dmsg
	Addr    address
}

type MockDispatcher struct {
	t *testing.T

	handlers map[reflect.Type]dispatcher.ProtocolHandler

	sentMessages chan addressedMsg
}

func NewMockDispatcher(t *testing.T) MockDispatcher {
	return MockDispatcher{
		t: t,

		handlers: make(map[reflect.Type]dispatcher.ProtocolHandler),

		sentMessages: make(chan addressedMsg, 100),
	}
}

func expectMessage(t *testing.T, expected dispatcher.Message, from address, actual addressedMsg) {
	if actual.Addr != from {
		t.Errorf("Expected message to %v, got %v", from, actual.Addr)
	}

	if actual.Payload != expected {
		t.Errorf("Expected message %v, got %v", expected, actual)
	}
}

func (d MockDispatcher) ExpectSentMessage(expected dispatcher.Message, from address) {
	expectMessage(d.GetTesting(), expected, from, d.getSentMessage())
}

func (d MockDispatcher) GetTesting() *testing.T {
	return d.t
}

func (d MockDispatcher) Register(msg dmsg, handler dispatcher.ProtocolHandler) {
	d.handlers[reflect.TypeOf(msg)] = handler
}

func (d MockDispatcher) Send(msg dmsg, dest address) {
	d.sentMessages <- addressedMsg{Payload: msg, Addr: dest}
}

func (d MockDispatcher) SendWithReceiptAck(msg dmsg, dest address, onReceipt func()) {
	d.t.Error("SendWithReceiptAck should not be called by ring manager")
}

func (d MockDispatcher) Close() {
}

func (d MockDispatcher) getSentMessage() addressedMsg {
	return <-d.sentMessages
}

func (d MockDispatcher) GetSentMessage() (dispatcher.Message, transport.Address) {
	sentMsg := d.getSentMessage()
	return sentMsg.Payload, sentMsg.Addr
}

func (d MockDispatcher) ExpectNothingFor(duration time.Duration) {
	select {
	case msg := <-d.sentMessages:
		d.t.Error("Expected no message to be sent; received", msg)
	case <-time.After(duration):
	}
}

func (d MockDispatcher) SimulateReception(msg dmsg, source address) {
	for msgType, handler := range d.handlers {
		if reflect.TypeOf(msg) == msgType {
			handler(msg, source)
			return
		}
	}
	d.t.Errorf("No handler found for message %v", msg)
}
