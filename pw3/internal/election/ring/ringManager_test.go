package ring

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/mocks"
	"chatsapp/internal/server/dispatcher"
	"chatsapp/internal/timestamps"
	"chatsapp/internal/transport"
	"testing"
	"time"
)

type mockMessage string

type addressedMsg struct {
	msg  dispatcher.Message
	addr transport.Address
}

func (mockMessage) RegisterToGob() {
}

func expectMessage(t *testing.T, payload mockMessage, msgType messageType, to address, actual addressedMsg) {
	ringMsg := actual.msg.(message)

	if actual.addr != to {
		t.Errorf("Expected message to %v, got %v", to, actual.addr)
	}

	if ringMsg.Type != msgType {
		t.Errorf("Expected message type %v, got %v", msgType, ringMsg.Type)
	}

	if msgType == payloadType {
		recvdMsg := ringMsg.Payload
		if recvdMsg != payload {
			t.Errorf("Expected message %v, got %v", payload, actual)
		}
	}
}

func expectSentMessage(d mocks.MockDispatcher, payload mockMessage, msgType messageType, from address) {
	msg, src := d.GetSentMessage()
	expectMessage(d.GetTesting(), payload, msgType, from, addressedMsg{msg, src})
}

func TestSendToNext(t *testing.T) {
	addrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5000},
		{IP: "127.0.0.1", Port: 5001},
		{IP: "127.0.0.1", Port: 5002},
	}

	logger := logging.NewStdLogger("test")
	dispatcher := mocks.NewMockDispatcher(t)
	ring := newRingMaintainer(logger, dispatcher, addrs[0], addrs, 500*time.Millisecond)

	ring.SendToNext(mockMessage("Hello, World!"))

	sentMsg, to := dispatcher.GetSentMessage()
	expectMessage(t, mockMessage("Hello, World!"), payloadType, addrs[1], addressedMsg{sentMsg, to})

	dispatcher.SimulateReception(message{ackMsg, sentMsg.(message).Timestamp, mockMessage("")}, addrs[1])

	dispatcher.ExpectNothingFor(1 * time.Second)
}

func TestReceiveFromPrev(t *testing.T) {
	addrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5000},
		{IP: "127.0.0.1", Port: 5001},
		{IP: "127.0.0.1", Port: 5002},
	}

	logger := logging.NewStdLogger("test")
	dispatcher := mocks.NewMockDispatcher(t)
	ring := newRingMaintainer(logger, dispatcher, addrs[0], addrs, 1*time.Second)

	ts := timestamps.NewLamportTimestampHandler("test", 0).GetTimestamp()
	msg := message{payloadType, ts, mockMessage("Hello, World!")}
	go dispatcher.SimulateReception(msg, addrs[2])

	deliveredMsg := ring.ReceiveFromPrev()
	if deliveredMsg != msg.Payload {
		t.Errorf("Expected delivered message to be '%v', got %v", msg.Payload, deliveredMsg)
	}

	sentMsg, to := dispatcher.GetSentMessage()
	expectMessage(t, mockMessage(""), ackMsg, addrs[2], addressedMsg{sentMsg, to})
}

func TestTimeout(t *testing.T) {
	addrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5000},
		{IP: "127.0.0.1", Port: 5001},
		{IP: "127.0.0.1", Port: 5002},
	}

	logger := logging.NewStdLogger("test")
	dispatcher := mocks.NewMockDispatcher(t)
	ring := newRingMaintainer(logger, dispatcher, addrs[0], addrs, 1*time.Second)

	ring.SendToNext(mockMessage("Hello, World!"))

	sentMsg, to := dispatcher.GetSentMessage()
	expectMessage(t, mockMessage("Hello, World!"), payloadType, addrs[1], addressedMsg{sentMsg, to})

	sentMsg, to = dispatcher.GetSentMessage()
	expectMessage(t, mockMessage("Hello, World!"), payloadType, addrs[2], addressedMsg{sentMsg, to})

	dispatcher.SimulateReception(message{ackMsg, sentMsg.(message).Timestamp, mockMessage("")}, addrs[2])

	dispatcher.ExpectNothingFor(2 * time.Second)
}

func TestRetriesFirstNeighborForNewMessage(t *testing.T) {
	addrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5000},
		{IP: "127.0.0.1", Port: 5001},
		{IP: "127.0.0.1", Port: 5002},
		{IP: "127.0.0.1", Port: 5003},
	}

	logger := logging.NewStdLogger("test")
	dispatcher := mocks.NewMockDispatcher(t)
	ring := newRingMaintainer(logger, dispatcher, addrs[0], addrs, 1*time.Second)

	// Sending a message to the next neighbor, which times out.
	ring.SendToNext(mockMessage("Hello, World!"))

	sentMsg, to := dispatcher.GetSentMessage()
	expectMessage(t, mockMessage("Hello, World!"), payloadType, addrs[1], addressedMsg{sentMsg, to})

	// Second next neighbor acks the message.
	sentMsg, to = dispatcher.GetSentMessage()
	expectMessage(t, mockMessage("Hello, World!"), payloadType, addrs[2], addressedMsg{sentMsg, to})

	dispatcher.SimulateReception(message{ackMsg, sentMsg.(message).Timestamp, mockMessage("")}, addrs[2])

	dispatcher.ExpectNothingFor(2 * time.Second)

	// New send request, should go to first neighbor.
	ring.SendToNext(mockMessage("Hello, World again!"))
	sentMsg, to = dispatcher.GetSentMessage()
	expectMessage(t, mockMessage("Hello, World again!"), payloadType, addrs[1], addressedMsg{sentMsg, to})

	dispatcher.SimulateReception(message{ackMsg, sentMsg.(message).Timestamp, mockMessage("")}, addrs[1])

	dispatcher.ExpectNothingFor(2 * time.Second)
}

func TestTimeoutLeadingToSelf(t *testing.T) {
	addrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5000}, // self
		{IP: "127.0.0.1", Port: 5001}, // will timeout
		{IP: "127.0.0.1", Port: 5002}, // will timeout
	}

	logger := logging.NewStdLogger("test")
	disp := mocks.NewMockDispatcher(t)
	ring := newRingMaintainer(logger, disp, addrs[0], addrs, 500*time.Millisecond)

	msg := mockMessage("Hello through timeout!")
	// Send a message - first attempt will go to Port 5001
	ring.SendToNext(msg)

	// First attempt to send to next node (5001)
	sentMsg, to := disp.GetSentMessage()
	expectMessage(t, msg, payloadType, addrs[1], addressedMsg{sentMsg, to})

	// After timeout, should try next node (5002)
	sentMsg, to = disp.GetSentMessage()
	expectMessage(t, msg, payloadType, addrs[2], addressedMsg{sentMsg, to})

	// After another timeout, should wrap around to self (5000)
	// Create a goroutine to wait for the message
	receivedMsg := ring.ReceiveFromPrev()
	if receivedMsg != msg {
		t.Errorf("Expected message 'Hello through timeout!', got %v", receivedMsg)
	}

	// Ensure no additional messages are sent
	disp.ExpectNothingFor(1 * time.Second)
}

func TestRingRotatesCorrectlyBeforeSending(t *testing.T) {
	// Create addresses with self at different positions to verify rotation works
	testCases := []struct {
		name         string
		addrs        []transport.Address
		self         transport.Address
		firstMessage transport.Address // The first node that should receive the message
	}{
		{
			name: "self_at_start",
			addrs: []transport.Address{
				{IP: "127.0.0.1", Port: 5000}, // self
				{IP: "127.0.0.1", Port: 5001},
				{IP: "127.0.0.1", Port: 5002},
			},
			self:         transport.Address{IP: "127.0.0.1", Port: 5000},
			firstMessage: transport.Address{IP: "127.0.0.1", Port: 5001},
		},
		{
			name: "self_in_middle",
			addrs: []transport.Address{
				{IP: "127.0.0.1", Port: 5001},
				{IP: "127.0.0.1", Port: 5000}, // self
				{IP: "127.0.0.1", Port: 5002},
			},
			self:         transport.Address{IP: "127.0.0.1", Port: 5000},
			firstMessage: transport.Address{IP: "127.0.0.1", Port: 5002},
		},
		{
			name: "self_at_end",
			addrs: []transport.Address{
				{IP: "127.0.0.1", Port: 5001},
				{IP: "127.0.0.1", Port: 5002},
				{IP: "127.0.0.1", Port: 5000}, // self
			},
			self:         transport.Address{IP: "127.0.0.1", Port: 5000},
			firstMessage: transport.Address{IP: "127.0.0.1", Port: 5001},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := logging.NewStdLogger("test")
			dispatcher := mocks.NewMockDispatcher(t)
			ring := newRingMaintainer(logger, dispatcher, tc.self, tc.addrs, 500*time.Millisecond)

			// Send a message to trigger the ring rotation and subsequent send
			msg := mockMessage("Test rotation")
			ring.SendToNext(msg)

			// Verify that the first message goes to the correct next node after self
			sentMsg, to := dispatcher.GetSentMessage()
			expectMessage(t, msg, payloadType, tc.firstMessage, addressedMsg{sentMsg, to})

			// Simulate successful ACK to prevent timeouts
			dispatcher.SimulateReception(message{ackMsg, sentMsg.(message).Timestamp, mockMessage("")}, tc.firstMessage)

			// Ensure no additional messages are sent (which would indicate incorrect rotation)
			dispatcher.ExpectNothingFor(1 * time.Second)
		})
	}
}
