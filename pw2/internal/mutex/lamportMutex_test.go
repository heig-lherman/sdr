package mutex

import (
	"chatsapp/internal/logging"
	"fmt"
	"testing"
	"time"
)

type MockMessage struct {
	From    Pid
	To      Pid
	Message Message
}

type mockMutexNetwork struct {
	sentMessages     chan MockMessage
	receivedMessages chan MockMessage
	neighbors        []Pid
	self             Pid
}

func NewMockMutexNetwork(self Pid, neighbors []Pid) *mockMutexNetwork {
	return &mockMutexNetwork{
		sentMessages:     make(chan MockMessage, 10),
		receivedMessages: make(chan MockMessage, 10),
		neighbors:        neighbors,
		self:             self,
	}
}

func (n *mockMutexNetwork) asNetWrapper() NetWrapper {
	outgoing := make(chan OutgoingMessage, 10)
	incoming := make(chan Message, 10)

	go func() {
		for {
			select {
			case m := <-outgoing:
				n.sentMessages <- MockMessage{From: n.self, To: m.Destination, Message: m.Message}
			case m, ok := <-n.receivedMessages:
				if !ok {
					return
				}
				incoming <- m.Message
			}
		}
	}()

	return NetWrapper{
		IntoNet: outgoing,
		FromNet: incoming,
	}
}

func (n *mockMutexNetwork) close() {
	close(n.sentMessages)
	close(n.receivedMessages)
}

func (n *mockMutexNetwork) Send(dest Pid, m Message) {
	fmt.Println("Mock intercepts sent message", m, "to", dest)
	n.sentMessages <- MockMessage{From: n.self, To: dest, Message: m}
}

func (n *mockMutexNetwork) Receive() (Message, bool) {
	m, ok := <-n.receivedMessages
	return m.Message, ok
}

func (n *mockMutexNetwork) SimulateReception(m MockMessage) {
	fmt.Println("Mock simulates reception of message", m, "from", m.Message.GetSource())
	n.receivedMessages <- m
}

// Compares expected and actual messages. Allows for messages to have different order and different seqnums. However, all expected seqnums should be present in the actual messages (only the order may differ).
func compareMessagesUnordered(t *testing.T, expected []MockMessage, actual []MockMessage) {
	if len(expected) != len(actual) {
		t.Fatal("Expected", expected, "got", actual)
		return
	}

	for _, e := range expected {
		messageFound := false
		seqnumFound := false
		for _, a := range actual {
			if !messageFound && e.From == a.From && e.To == a.To && e.Message.Type == a.Message.Type && e.Message.TS.Pid == a.Message.TS.Pid {
				messageFound = true
			}
			if !seqnumFound && e.Message.TS.Seqnum == a.Message.TS.Seqnum {
				seqnumFound = true
			}
		}
		if !messageFound || !seqnumFound {
			t.Fatal("Expected messages", expected, "not all found. Had messages", actual)
			return
		}
	}
}

/** Waits timeout to receive the expected messages, in arbitrary order */
func expectMessages(t *testing.T, network *mockMutexNetwork, expected []MockMessage, timeout time.Duration) {
	to := time.After(timeout)

	received := []MockMessage{}
	for i := 0; i < len(expected); i++ {
		select {
		case actual := <-network.sentMessages:
			received = append(received, actual)
		case <-to:
			t.Fatalf("Did not receive all expected messages. Expected %v, got %v", expected, received)
		}
	}

	compareMessagesUnordered(t, expected, received)
}

func expectNothing(t *testing.T, network *mockMutexNetwork, timeout time.Duration) {
	select {
	case msg := <-network.sentMessages:
		t.Fatal("Expected no messages to be sent ; yet received", msg)
	case <-time.After(timeout):
	}
}

func newLogger() *logging.Logger {
	return logging.NewStdLogger("test")
}

func TestSingleProcess(t *testing.T) {
	mockNetwork := NewMockMutexNetwork("A", []Pid{})
	defer mockNetwork.close()
	m := NewLamportMutex(newLogger(), mockNetwork.asNetWrapper(), "A", []Pid{})

	release, err := m.Request()
	if err != nil {
		t.Error("Error requesting mutex:", err)
	}

	release()

	expectNothing(t, mockNetwork, 1*time.Second)
}

func TestWithOneNeighbor(t *testing.T) {
	mockNetwork := NewMockMutexNetwork("A", []Pid{"B"})
	defer mockNetwork.close()
	m := NewLamportMutex(newLogger(), mockNetwork.asNetWrapper(), "A", []Pid{"B"})

	go func() {
		release, err := m.Request()
		if err != nil {
			t.Error("Error requesting mutex:", err)
		}
		time.Sleep(1 * time.Second)
		release()
	}()

	// Should send request
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 1, Pid: "A"}}},
	}, 5*time.Second)

	expectNothing(t, mockNetwork, 1*time.Second)

	// Simulate reception of ACK for that request
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 2, Pid: "B"}}})

	// Should send release
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 4, Pid: "A"}}},
	}, 5*time.Second)
}

func TestWaitsAllAcks(t *testing.T) {
	mockNetwork := NewMockMutexNetwork("A", []Pid{"B", "C"})
	defer mockNetwork.close()
	m := NewLamportMutex(newLogger(), mockNetwork.asNetWrapper(), "A", []Pid{"B", "C"})

	go func() {
		release, err := m.Request()
		if err != nil {
			t.Error("Error requesting mutex:", err)
		}
		release()
	}()

	// Should send two REQ requests
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 1, Pid: "A"}}},
		{From: "A", To: "C", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 1, Pid: "A"}}},
	}, 5*time.Second)

	// Simulate reception of ACK from B
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 2, Pid: "B"}}})

	// Should not send REL yet
	expectNothing(t, mockNetwork, 1*time.Second)

	// Simulate reception of ACK from C
	mockNetwork.SimulateReception(MockMessage{From: "C", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 3, Pid: "C"}}})

	// Should receive two releases
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 5, Pid: "A"}}},
		{From: "A", To: "C", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 5, Pid: "A"}}},
	}, 5*time.Second)
}

func TestRecvReq(t *testing.T) {
	// 'A' receives REQ from 'B', 'A' should send ACK to 'B'.

	mockNetwork := NewMockMutexNetwork("A", []Pid{"B"})
	defer mockNetwork.close()
	NewLamportMutex(newLogger(), mockNetwork.asNetWrapper(), "A", []Pid{"B"})

	// Simulate reception of REQ from B
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 1, Pid: "B"}}})

	// A should send ACK
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 2, Pid: "A"}}},
	}, 5*time.Second)
}
