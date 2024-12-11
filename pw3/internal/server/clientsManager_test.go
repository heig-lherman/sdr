package server

import (
	"chatsapp/internal/common"
	"chatsapp/internal/election"
	"chatsapp/internal/logging"
	"chatsapp/internal/mocks"
	"chatsapp/internal/transport"
	"testing"
	"time"
)

type mockElector struct {
	startElection chan struct{}
	endElection   chan transport.Address
	getLeader     chan transport.Address

	setAbility chan election.Ability
	getAbility chan election.Ability
}

func newMockElector(ability election.Ability, leader transport.Address) *mockElector {
	e := mockElector{
		startElection: make(chan struct{}),
		endElection:   make(chan transport.Address),
		getLeader:     make(chan transport.Address),
		setAbility:    make(chan election.Ability),
		getAbility:    make(chan election.Ability),
	}

	go func() {
		leader := leader
		inElection := false
		for {
			if inElection {
				select {
				case newLeader := <-e.endElection:
					inElection = false
					leader = newLeader
				}
			} else {
				select {
				case <-e.startElection:
					inElection = true
				case e.getLeader <- leader:
				}
			}
		}
	}()

	go func() {
		ability := ability
		for {
			select {
			case newAbility := <-e.setAbility:
				ability = newAbility
			case e.getAbility <- ability:
			}
		}
	}()

	return &e
}

func (m *mockElector) SimulateElectionStart() {
	m.startElection <- struct{}{}
}

func (m *mockElector) SimulateElectionEnd(leader transport.Address) {
	m.endElection <- leader
}

func (m *mockElector) GetLeader() transport.Address {
	return <-m.getLeader
}

func (m *mockElector) UpdateAbility(ability election.Ability) {
	m.setAbility <- ability
}

func (m *mockElector) expectAbility(t *testing.T, expected election.Ability) {
	if ability := <-m.getAbility; ability != expected {
		t.Error("Expected ability ", expected, ", got ", ability)
	}
}

func TestConnReqToLeader(t *testing.T) {
	logger := logging.NewStdLogger("t")
	elector := newMockElector(0, addrs[0])
	disp := mocks.NewMockDispatcher(t)
	cm := NewClientManager(logger, addrs[0], elector, disp)

	clientAddr := transport.Address{IP: "127.0.0.1", Port: 7001}

	connReq := common.ConnRequestMessage{User: "Alex"}
	disp.SimulateReception(connReq, clientAddr)

	responseMsg, _ := disp.GetSentMessage()
	if connResp, ok := responseMsg.(common.ConnResponseMessage); !ok {
		t.Error("Response message is not a ConnResponseMessage: ", responseMsg)
	} else {
		if connResp.Leader != addrs[0] {
			t.Error("Expected leader to be ", addrs[0], ", got ", connResp.Leader)
		}
	}
	time.Sleep(100 * time.Millisecond)
	elector.expectAbility(t, -1)

	// Expect no received message from clientManager for 1 second
	receivedMessages := make(chan common.ChatMessage, 1)
	go func() {
		for {
			msg, err := cm.ReceiveMessage()
			if err != nil {
				t.Error("Error receiving message:", err)
				break
			}
			receivedMessages <- msg
		}
	}()
	select {
	case <-receivedMessages:
		t.Error("Received message from clientManager, expected no message")
	case <-time.After(time.Second):
	}

	// client sends message
	clientMsg := common.ChatMessage{User: "Alex", Content: "Hello, World!"}
	disp.SimulateReception(clientMsg, clientAddr)
	// Expect received message from clientManager
	select {
	case msg := <-receivedMessages:
		if msg.Content != "Hello, World!" {
			t.Error("Expected message content to be 'Hello, World!', got:", msg.Content)
		}
		if msg.User != "Alex" {
			t.Error("Expected message user to be 'Alex', got:", msg.User)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for message from clientManager")
	}

	// client closes connection
	closeMsg := common.ConnClose{User: "Alex"}
	disp.SimulateReception(closeMsg, clientAddr)
	time.Sleep(500 * time.Millisecond)
	elector.expectAbility(t, 0)

	// client sends chat message; should be ignored
	disp.SimulateReception(clientMsg, clientAddr)
	select {
	case <-receivedMessages:
		t.Error("Received message from clientManager, expected no message")
	case <-time.After(time.Second):
	}
}

func TestConnReqToNonLeader(t *testing.T) {
	logger := logging.NewStdLogger("t")
	elector := newMockElector(0, addrs[2])
	disp := mocks.NewMockDispatcher(t)
	cm := NewClientManager(logger, addrs[0], elector, disp)

	connReq := common.ConnRequestMessage{User: "Alex"}
	disp.SimulateReception(connReq, addrs[1])

	// Expect a connResp with actual leader
	responseMsg, _ := disp.GetSentMessage()
	if connResp, ok := responseMsg.(common.ConnResponseMessage); !ok {
		t.Error("Response message is not a ConnResponseMessage: ", responseMsg)
	} else {
		if connResp.Leader != addrs[2] {
			t.Error("Expected leader to be ", addrs[2], ", got ", connResp.Leader)
		}
	}
	elector.expectAbility(t, 0)

	// Expect no received message from clientManager for 1 second
	receivedMessages := make(chan common.ChatMessage, 1)
	go func() {
		msg, err := cm.ReceiveMessage()
		if err != nil {
			t.Error("Error receiving message:", err)
		}
		receivedMessages <- msg
	}()
	select {
	case <-receivedMessages:
		t.Error("Received message from clientManager, expected no message")
	case <-time.After(time.Second):
	}

	// client sends message
	clientMsg := common.ChatMessage{User: "Alex", Content: "Hello, World!"}
	disp.SimulateReception(clientMsg, addrs[1])
	// Expect message to be ingored by clientManager, i.e. not delivered to app

	select {
	case <-receivedMessages:
		t.Error("Received message from clientManager, expected no message")
	case <-time.After(time.Second):
	}
}

func TestConcurrentConnReqs(t *testing.T) {
	logger := logging.NewStdLogger("t")
	elector := newMockElector(0, addrs[0])
	disp := mocks.NewMockDispatcher(t)
	NewClientManager(logger, addrs[0], elector, disp)

	clientAddr1 := transport.Address{IP: "127.0.0.1", Port: 7001}
	clientAddr2 := transport.Address{IP: "127.0.0.1", Port: 7002}

	connReq1 := common.ConnRequestMessage{User: "Alex"}
	connReq2 := common.ConnRequestMessage{User: "Bob"}

	disp.SimulateReception(connReq1, clientAddr1)
	disp.SimulateReception(connReq2, clientAddr2)

	// Expect a connResp with actual leader
	responseMsg1, _ := disp.GetSentMessage()
	if connResp, ok := responseMsg1.(common.ConnResponseMessage); !ok {
		t.Error("Response message is not a ConnResponseMessage: ", responseMsg1)
	} else {
		if connResp.Leader != addrs[0] {
			t.Error("Expected leader to be ", addrs[0], ", got ", connResp.Leader)
		}
	}

	responseMsg2, _ := disp.GetSentMessage()
	if connResp, ok := responseMsg2.(common.ConnResponseMessage); !ok {
		t.Error("Response message is not a ConnResponseMessage: ", responseMsg2)
	} else {
		if connResp.Leader != addrs[0] {
			t.Error("Expected leader to be ", addrs[0], ", got ", connResp.Leader)
		}
	}

	time.Sleep(100 * time.Millisecond)

	elector.expectAbility(t, -2)
}

func TestConnReqDuringElection(t *testing.T) {
	logger := logging.NewStdLogger("t")
	elector := newMockElector(0, addrs[0])
	disp := mocks.NewMockDispatcher(t)
	NewClientManager(logger, addrs[0], elector, disp)

	clientAddr1 := transport.Address{IP: "127.0.0.1", Port: 7001}
	clientAddr2 := transport.Address{IP: "127.0.0.1", Port: 7002}

	connReq1 := common.ConnRequestMessage{User: "Alex"}
	connReq2 := common.ConnRequestMessage{User: "Bob"}

	elector.SimulateElectionStart()

	disp.SimulateReception(connReq1, clientAddr1)
	go disp.SimulateReception(connReq2, clientAddr2)

	// Expect nothing for 1 second
	disp.ExpectNothingFor(time.Second)

	elector.SimulateElectionEnd(addrs[1])

	// Expect a connResp with actual leader for client 1
	responseMsg1, _ := disp.GetSentMessage()
	if connResp, ok := responseMsg1.(common.ConnResponseMessage); !ok {
		t.Error("Response message is not a ConnResponseMessage: ", responseMsg1)
	} else {
		if connResp.Leader != addrs[1] {
			t.Error("Expected leader to be ", addrs[1], ", got ", connResp.Leader)
		}
	}

	elector.expectAbility(t, 0)

	// Expect a connResp with actual leader for client 2
	responseMsg2, _ := disp.GetSentMessage()
	if connResp, ok := responseMsg2.(common.ConnResponseMessage); !ok {
		t.Error("Response message is not a ConnResponseMessage: ", responseMsg2)
	} else {
		if connResp.Leader != addrs[1] {
			t.Error("Expected leader to be ", addrs[1], ", got ", connResp.Leader)
		}
	}
}
