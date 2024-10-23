package tcp

import (
	"chatsapp/internal/logging"
	"testing"
	"time"
)

type handler struct {
	t               *testing.T
	expectedMessage chan *Message
}

func (h handler) HandleNetworkMessage(m *Message) (wasHandled bool) {
	expected := <-h.expectedMessage
	if m.Source != expected.Source {
		h.t.Errorf("Expected source %v, got %v", expected.Source, m.Source)
	}
	if string(m.Payload) != string(expected.Payload) {
		h.t.Errorf("Expected payload %v, got %v", expected.Payload, m.Payload)
	}
	return true
}

func newHandler(t *testing.T) handler {
	expectedMessage := make(chan *Message)
	return handler{t: t, expectedMessage: expectedMessage}
}

func (h handler) expectMessage(m *Message) {
	h.expectedMessage <- m
}

func (h handler) expectNoMessageFor(d time.Duration) {
	m := &Message{}
	select {
	case h.expectedMessage <- m:
		h.t.Errorf("Was not expecting a message")
	case <-time.After(d):
	}
}

func TestSend(t *testing.T) {
	addrs := []Address{
		{IP: "127.0.0.1", Port: 5000},
		{IP: "127.0.0.1", Port: 5001},
		{IP: "127.0.0.1", Port: 5002},
	}

	log0 := logging.NewStdLogger("0")
	log1 := logging.NewStdLogger("1")
	log2 := logging.NewStdLogger("2")

	tcp0 := NewTCP(addrs[0], []Address{addrs[1], addrs[2]}, log0)
	tcp1 := NewTCP(addrs[1], []Address{addrs[0], addrs[2]}, log1)
	tcp2 := NewTCP(addrs[2], []Address{addrs[0], addrs[1]}, log2)

	h0 := newHandler(t)
	h1 := newHandler(t)
	h2 := newHandler(t)

	h0_id := tcp0.RegisterHandler(h0)
	h1_id := tcp1.RegisterHandler(h1)
	h2_id := tcp2.RegisterHandler(h2)

	payload1 := []byte{42}
	payload2 := []byte{43}
	payload3 := []byte{44}
	go tcp0.Send(addrs[1], payload1)
	go tcp1.Send(addrs[2], payload2)
	go tcp2.Send(addrs[0], payload3)

	h1.expectMessage(&Message{Source: addrs[0], Payload: payload1})
	h2.expectMessage(&Message{Source: addrs[1], Payload: payload2})
	h0.expectMessage(&Message{Source: addrs[2], Payload: payload3})

	payload4 := []byte{45}
	tcp2.Send(addrs[1], payload4)
	h1.expectMessage(&Message{Source: addrs[2], Payload: payload4})

	tcp0.UnregisterHandler(h0_id)
	tcp1.UnregisterHandler(h1_id)
	tcp2.UnregisterHandler(h2_id)

	tcp0.Close()
	tcp1.Close()
	tcp2.Close()

	time.Sleep(100 * time.Millisecond)
}

func TestClose(t *testing.T) {
	// s1 sends to s2, implicitly creating a connection,
	// then s1 closes the connection,
	// then s2 tries to send to s1, but the connection is closed; should create a new connection.
	// s2 closes the connection
	// s2 tries to send to s1, but the connection is closed; should create a new connection.

	addrs := []Address{
		{IP: "127.0.0.1", Port: 5000},
		{IP: "127.0.0.1", Port: 5001},
	}

	log0 := logging.NewStdLogger("0")
	log1 := logging.NewStdLogger("1")

	tcp0 := NewTCP(addrs[0], []Address{addrs[1]}, log0)
	tcp1 := NewTCP(addrs[1], []Address{addrs[0]}, log1)

	h0 := newHandler(t)
	h1 := newHandler(t)

	tcp0.RegisterHandler(h0)
	tcp1.RegisterHandler(h1)

	payloads := [][]byte{
		{42},
		{43},
	}

	tcp0.Send(addrs[1], payloads[0])
	h1.expectMessage(&Message{Source: addrs[0], Payload: payloads[0]})

	tcp0.Close()
	time.Sleep(100 * time.Millisecond)
	log0 = logging.NewStdLogger("0b")
	tcp0 = NewTCP(addrs[0], []Address{addrs[1]}, log0)
	tcp0.RegisterHandler(h0)
	time.Sleep(100 * time.Millisecond)

	tcp1.Send(addrs[0], payloads[1])
	h0.expectMessage(&Message{Source: addrs[1], Payload: payloads[1]})

	tcp1.Close()
	time.Sleep(100 * time.Millisecond)
	log1 = logging.NewStdLogger("1b")
	tcp1 = NewTCP(addrs[1], []Address{addrs[0]}, log1)
	tcp1.RegisterHandler(h1)
	time.Sleep(100 * time.Millisecond)

	tcp1.Send(addrs[0], payloads[0])
	h0.expectMessage(&Message{Source: addrs[1], Payload: payloads[0]})
}
