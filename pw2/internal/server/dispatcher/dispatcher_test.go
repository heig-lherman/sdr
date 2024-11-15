package dispatcher

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils/ioUtils"
	"encoding/gob"
	"reflect"
	"testing"
)

type MutexMessage struct {
	Pid uint32
}

type ChatMessage struct {
	Content string
}

func (MutexMessage) RegisterToGob() {
	gob.Register(MutexMessage{})
}

func (ChatMessage) RegisterToGob() {
	gob.Register(ChatMessage{})
}

func GetRandomMessage() Message {
	return MutexMessage{}
}

func TestRegister(t *testing.T) {
	addr1 := transport.Address{IP: "127.0.0.1", Port: 5000}
	addr2 := transport.Address{IP: "127.0.0.1", Port: 5001}

	expectedMsg := MutexMessage{42}

	mockNet := transport.NewMockNetworkInterface()

	logger := logging.NewLogger(ioUtils.NewStdStream(), nil, "disp_test", false)
	d := NewDispatcher(logger, addr1, mockNet)
	d.Register(MutexMessage{}, func(msg Message, source transport.Address) {
		if _, ok := msg.(MutexMessage); !ok {
			t.Fatalf("expected MutexMessage, got %T", msg)
		}

		if source != addr2 {
			t.Fatalf("expected source %v, got %v", addr1, source)
		}

		if !reflect.DeepEqual(msg, expectedMsg) {
			t.Fatalf("expected message %v, got %v", expectedMsg, msg)
		}
	})

	go func() {
		receivedMsg := <-mockNet.SentMessages
		mockNet.ReceiveFromNetwork(&receivedMsg.Message)
	}()

	d.Send(expectedMsg, addr2)
}
