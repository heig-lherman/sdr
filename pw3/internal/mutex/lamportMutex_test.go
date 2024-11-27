package mutex

import (
	"chatsapp/internal/logging"
	"fmt"
	"sync"
	"sync/atomic"
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

func TestConcurrentRequests(t *testing.T) {
	// 2 neighbors.

	mockNetwork := NewMockMutexNetwork("A", []Pid{"B", "C"})
	defer mockNetwork.close()
	m := NewLamportMutex(newLogger(), mockNetwork.asNetWrapper(), "A", []Pid{"B", "C"})

	mayRelease := make(chan struct{})

	go func() {
		fmt.Println("A requests mutex...")
		release, err := m.Request()
		if err != nil {
			t.Error("Error requesting mutex:", err)
		}
		fmt.Println("A is in CS...")

		<-mayRelease

		fmt.Println("A is done with CS, releasing...")
		release()

		fmt.Println("A requests mutex again...")
		release, err = m.Request()
		if err != nil {
			t.Error("Error requesting mutex second time:", err)
		}

		fmt.Println("A is in CS again... And releasing immediately...")
		release()
	}()

	// A should send 2 REQs
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 1, Pid: "A"}}},
		{From: "A", To: "C", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 1, Pid: "A"}}},
	}, 5*time.Second)
	// Simulate reception of ACK from C
	mockNetwork.SimulateReception(MockMessage{From: "C", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 3, Pid: "C"}}})

	// A is missing B's ACK, should not send REL yet
	expectNothing(t, mockNetwork, 2*time.Second)

	// Simulate reception of REQ from B
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 5, Pid: "B"}}})

	// A should send ACK to B's REQ, and enter CS.
	fmt.Println("A received B's REQ, should send ACK (and enter CS)")
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 6, Pid: "A"}}},
	}, 5*time.Second)

	// Simulate reception of ACK from B (as response to A's REQ)
	fmt.Println("B only now sends ACK for A's REQ. A should ignore.")
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 6, Pid: "B"}}})

	// Should not cause A to do anything
	expectNothing(t, mockNetwork, 2*time.Second)
	fmt.Println("A successfully ignored ACK.")

	// A may release
	fmt.Println("A may release")
	mayRelease <- struct{}{}

	// A should release and thus send REL
	fmt.Println("A had oldest message so should have entered. It should now release, so should send RELs")
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 8, Pid: "A"}}},
		{From: "A", To: "C", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 8, Pid: "A"}}},
	}, 5*time.Second)

	// A requests again; should send 2 REQs
	fmt.Println("A should request again, so send 2 REQs")
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 9, Pid: "A"}}},
		{From: "A", To: "C", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 9, Pid: "A"}}},
	}, 5*time.Second)

	// Simulate reception of ACK from C
	mockNetwork.SimulateReception(MockMessage{From: "C", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 12, Pid: "C"}}})

	// A is missing B's message; should not send REL yet because should not enter CS.
	fmt.Println("A does not have priority; B has. Hence should not send REL yet")
	expectNothing(t, mockNetwork, 2*time.Second)
	fmt.Println("A correctly did not send REL")

	// Simulate reception of ACK from B; B is still in CS
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 10, Pid: "B"}}})

	// A should not have entered CS, and thus not send REL.
	fmt.Println("A has both ACKs but B's is newer, so should not send REL yet")
	expectNothing(t, mockNetwork, 2*time.Second)
	fmt.Println("A correctly did not send REL")

	// Simulate reception of REL from B
	fmt.Println("B releases")
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 11, Pid: "B"}}})

	// A should send REL
	fmt.Println("A has oldest REQ ; should send REL")
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 16, Pid: "A"}}},
		{From: "A", To: "C", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 16, Pid: "A"}}},
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

func TestRecvReqWhileInCS(t *testing.T) {
	// 'A' sends REQ,
	// 'B' replies ACK,
	// 'A' enters CS for a long time.
	// 'A' receives REQ from 'B',
	// 'A' should send ACK to 'B' before sending REL.

	mockNetwork := NewMockMutexNetwork("A", []Pid{"B"})
	defer mockNetwork.close()
	m := NewLamportMutex(newLogger(), mockNetwork.asNetWrapper(), "A", []Pid{"B"})

	mayRelease := make(chan struct{})

	go func() {
		release, err := m.Request()
		if err != nil {
			t.Error("Error requesting mutex:", err)
		}

		<-mayRelease

		release()
	}()

	// Should send REQ
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 1, Pid: "A"}}},
	}, 5*time.Second)

	// Simulate reception of ACK from B
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 2, Pid: "B"}}})

	// Simulate reception of REQ from B
	mockNetwork.SimulateReception(MockMessage{From: "B", To: "A", Message: Message{Type: reqMsg, TS: timestamp{Seqnum: 3, Pid: "B"}}})

	// A should send ACK
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: ackMsg, TS: timestamp{Seqnum: 4, Pid: "A"}}},
	}, 5*time.Second)

	close(mayRelease)

	// Should send REL
	expectMessages(t, mockNetwork, []MockMessage{
		{From: "A", To: "B", Message: Message{Type: relMsg, TS: timestamp{Seqnum: 5, Pid: "A"}}},
	}, 5*time.Second)
}

type chanNet struct {
	fromMutexChans map[Pid]chan OutgoingMessage
	intoMutexChans map[Pid]chan Message
	toDispatch     chan OutgoingMessage
}

func newChanNet(procs []Pid) *chanNet {
	fromMutexChans := make(map[Pid]chan OutgoingMessage)
	intoMutexChans := make(map[Pid]chan Message)

	for _, p := range procs {
		fromMutexChans[p] = make(chan OutgoingMessage, 10)
		intoMutexChans[p] = make(chan Message, 10)
	}

	n := chanNet{
		fromMutexChans: fromMutexChans,
		intoMutexChans: intoMutexChans,
		toDispatch:     make(chan OutgoingMessage, 1000),
	}

	go n.centralize()

	return &n
}

func (n *chanNet) centralize() {
	for _, ch := range n.fromMutexChans {
		go func(ch chan OutgoingMessage) {
			for msg := range ch {
				n.toDispatch <- msg
			}
		}(ch)
	}

	for msg := range n.toDispatch {
		n.intoMutexChans[msg.Destination] <- msg.Message
	}
}

func (n *chanNet) close() {
	for _, ch := range n.fromMutexChans {
		close(ch)
	}
	close(n.toDispatch)
}

func (n *chanNet) getNetWrapper(proc Pid) NetWrapper {
	if _, ok := n.fromMutexChans[proc]; !ok {
		panic("No such process")
	}
	return NetWrapper{
		IntoNet: n.fromMutexChans[proc],
		FromNet: n.intoMutexChans[proc],
	}
}

func TestSpam(t *testing.T) {
	numProcs := 10
	numReqs := 100

	procs := make([]Pid, numProcs)
	for i := 0; i < numProcs; i++ {
		procs[i] = Pid(fmt.Sprintf("P%d", i))
	}

	net := newChanNet(procs)
	defer net.close()

	mutexes := make(map[Pid]Mutex)
	for _, p := range procs {
		neighbors := make([]Pid, 0)
		for _, n := range procs {
			if n != p {
				neighbors = append(neighbors, n)
			}
		}
		mutexes[p] = NewLamportMutex(newLogger().WithPostfix(string(p)), net.getNetWrapper(p), p, neighbors)
	}

	var wg sync.WaitGroup
	wg.Add(len(procs))

	for _, p := range procs {
		t.Logf("Starting process %v", p)
		go func(p Pid, m Mutex) {
			defer wg.Done()
			for i := 0; i < numReqs; i++ {
				release, err := m.Request()
				if err != nil {
					t.Error("Error requesting mutex:", err)
				}
				time.Sleep(1 * time.Millisecond)
				release()
			}
			t.Logf("Process %v done", p)
		}(p, mutexes[p])
	}

	wg.Wait()

	time.Sleep(1 * time.Second)
}

func TestSpamNoOverlap(t *testing.T) {
	numProcs := 10
	numReqs := 100

	var count int32

	procs := make([]Pid, numProcs)
	for i := 0; i < numProcs; i++ {
		procs[i] = Pid(fmt.Sprintf("P%d", i))
	}

	net := newChanNet(procs)
	defer net.close()

	mutexes := make(map[Pid]Mutex)
	for _, p := range procs {
		neighbors := make([]Pid, 0)
		for _, n := range procs {
			if n != p {
				neighbors = append(neighbors, n)
			}
		}
		mutexes[p] = NewLamportMutex(newLogger().WithPostfix(string(p)), net.getNetWrapper(p), p, neighbors)
	}

	var wg sync.WaitGroup
	wg.Add(len(procs))

	for _, p := range procs {
		t.Logf("Starting process %v", p)
		go func(p Pid, m Mutex) {
			defer wg.Done()
			for i := 0; i < numReqs; i++ {
				release, err := m.Request()
				new := atomic.AddInt32(&count, 1)
				if new != 1 {
					t.Errorf("Expected count to be 1, got %v", new)
				}
				if err != nil {
					t.Error("Error requesting mutex:", err)
				}
				time.Sleep(1 * time.Millisecond)

				new = atomic.AddInt32(&count, -1)
				if new != 0 {
					t.Errorf("Expected count to be 1, got %v", new)
				}

				release()
			}
			t.Logf("Process %v done", p)
		}(p, mutexes[p])
	}

	wg.Wait()

	time.Sleep(1 * time.Second)
}
