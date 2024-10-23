package server

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/transport/tcp"
	"chatsapp/internal/utils/ioUtils"
	"fmt"
	"strconv"
	"testing"
	"time"
)

var addrs = []transport.Address{
	{IP: "127.0.0.1", Port: 5000},
	{IP: "127.0.0.1", Port: 5001},
	{IP: "127.0.0.1", Port: 5002},
}

func createServer(t *testing.T, logFileName string, user Username, addr transport.Address, neighbors []transport.Address, printReadAck bool, slowdownMs uint32) (ioStream ioUtils.MockIOStream, server *Server, networkInterface transport.NetworkInterface) {
	debug := true
	ioStream = ioUtils.NewMockReader()
	logFile := logging.NewLogFile(logFileName)
	log := logging.NewLogger(ioStream, logFile, strconv.Itoa(int(addr.Port)), false)
	// networkInterface = transport.NewUDPInterface(addr, log)
	networkInterface = tcp.NewTCP(addr, neighbors, log.WithPostfix("tcp").WithLogLevel(logging.WARN))
	log.Infof("Creating server at %s with neighbors %v", addr, neighbors)
	server = newServer(ioStream, log, debug, addr, user, neighbors, printReadAck, networkInterface, slowdownMs)
	go server.Start()
	t.Cleanup(func() {
		server.Close()
		networkInterface.Close()
		fmt.Println("Closing server")
	})
	return
}

func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func TestUniqueMessage(t *testing.T) {
	input := "Hello, I'm Alice!"

	i1, _, _ := createServer(t, "./server1.log", "Alice", addrs[0], []transport.Address{addrs[1]}, true, 0)
	i2, _, _ := createServer(t, "./server2.log", "Bob", addrs[1], []transport.Address{addrs[0]}, true, 0)

	i1.SimulateNextInputLine(input)
	expected := "Alice: " + input + "\n"
	s := i2.InterceptNextPrintln()
	if s != expected {
		t.Errorf("Expected %s, got %s", expected, s)
	}
}

func TestMultipleUnidirectionalMessages(t *testing.T) {
	inputs := []string{
		"Hello, I'm Alice!",
		"Hi Bob!",
		"Hi Bob again!",
	}

	i1, _, _ := createServer(t, "./server1.log", "Alice", addrs[0], []transport.Address{addrs[1]}, true, 0)
	i2, _, _ := createServer(t, "./server2.log", "Bob", addrs[1], []transport.Address{addrs[0]}, true, 0)

	for _, input := range inputs {
		i1.SimulateNextInputLine(input)
		expected := "Alice: " + input + "\n"
		s := i2.InterceptNextPrintln()
		if s != expected {
			t.Errorf("Expected %s, got %s", expected, s)
		}
	}
}

func TestServerBidirectionalMessages(t *testing.T) {
	s1Inputs := []string{
		"Hello, I'm Alice!",
		"Hi Bob!",
		"Hi Bob again!",
	}

	s2Inputs := []string{
		"Hi, I'm Bob.",
		"Hi Alice",
	}

	i1, _, _ := createServer(t, "./server1.log", "Alice", addrs[0], []transport.Address{addrs[1]}, false, 0)
	i2, _, _ := createServer(t, "./server2.log", "Bob", addrs[1], []transport.Address{addrs[0]}, false, 0)

	for _, input := range s1Inputs {
		i1.SimulateNextInputLine(input)
		expected := "Alice: " + input + "\n"
		s := i2.InterceptNextPrintln()
		if s != expected {
			t.Errorf("Expected %s, got %s", expected, s)
		}
	}

	for _, input := range s2Inputs {
		i2.SimulateNextInputLine(input)
		expected := "Bob: " + input + "\n"
		s := i1.InterceptNextPrintln()
		if s != expected {
			t.Errorf("Expected %s, got %s", expected, s)
		}
	}
}

/** Test : Servers send messages one after the other */
func TestServerReadAck(t *testing.T) {
	s1Inputs := []string{
		"Hello, I'm Alice!",
	}

	s2Inputs := []string{
		"Hi, I'm Bob.",
		"Nice to meet you, Alice!",
	}

	i1, _, _ := createServer(t, "./server1.log", "Alice", addrs[0], []transport.Address{addrs[1]}, true, 0)
	i2, _, _ := createServer(t, "./server2.log", "Bob", addrs[1], []transport.Address{addrs[0]}, true, 0)

	for _, input := range s1Inputs {
		i1.SimulateNextInputLine(input)
		expected := "Alice: " + input + "\n"
		s := i2.InterceptNextPrintln()
		if s != expected {
			t.Errorf("Expected %s, got %s", expected, s)
		}
		s = i1.InterceptNextPrintln()
		expected = "[127.0.0.1:5001 received: " + input + "]\n"
		if s != expected {
			t.Errorf("Expected %s, got %s", expected, s)
		}

	}

	for _, input := range s2Inputs {
		i2.SimulateNextInputLine(input)
		expected := "Bob: " + input + "\n"
		s := i1.InterceptNextPrintln()
		if s != expected {
			t.Errorf("Expected >%s<, got >%s<", expected, s)
		}
		s = i2.InterceptNextPrintln()
		expected = "[127.0.0.1:5000 received: " + input + "]\n"
		if s != expected {
			t.Errorf("Expected >%s<, got >%s<", expected, s)
		}
	}
}

/** Test : Servers spam messages to each other */
func TestServerSpamMessages(t *testing.T) {
	s1Inputs := []string{
		"Hello, I'm Alice!",
		"Hi Bob!",
		"Hi Bob again!",
	}

	s2Inputs := []string{
		"Hi, I'm Bob.",
		"Hi Alice",
	}

	s3Inputs := []string{
		"Hi, I'm Charlie.",
		"Hi Alice from Charlie",
	}

	// Create two connected servers
	i1, _, _ := createServer(t, "./server1.log", "Alice", addrs[0], []transport.Address{addrs[1], addrs[2]}, false, 0)

	i2, _, _ := createServer(t, "./server2.log", "Bob", addrs[1], []transport.Address{addrs[0], addrs[2]}, false, 0)

	i3, _, _ := createServer(t, "./server3.log", "Charlie", addrs[2], []transport.Address{addrs[0], addrs[1]}, false, 0)

	expectedFromAlice := []string{}
	for _, input := range s1Inputs {
		i1.SimulateNextInputLine(input)
		expectedFromAlice = append(expectedFromAlice, "Alice: "+input+"\n")
	}
	expectedFromBob := []string{}
	for _, input := range s2Inputs {
		i2.SimulateNextInputLine(input)
		expectedFromBob = append(expectedFromBob, "Bob: "+input+"\n")
	}
	expectedFromCharlie := []string{}
	for _, input := range s3Inputs {
		i3.SimulateNextInputLine(input)
		expectedFromCharlie = append(expectedFromCharlie, "Charlie: "+input+"\n")
	}

	aliceExpectedCount := len(expectedFromBob) + len(expectedFromCharlie)
	bobExpectedCount := len(expectedFromAlice) + len(expectedFromCharlie)
	charlieExpectedCount := len(expectedFromAlice) + len(expectedFromBob)

	aliceReceived := []string{}
	for i := 0; i < aliceExpectedCount; i++ {
		aliceReceived = append(aliceReceived, i1.InterceptNextPrintln())
	}
	bobReceived := []string{}
	for i := 0; i < bobExpectedCount; i++ {
		bobReceived = append(bobReceived, i2.InterceptNextPrintln())
	}
	charlieReceived := []string{}
	for i := 0; i < charlieExpectedCount; i++ {
		charlieReceived = append(charlieReceived, i3.InterceptNextPrintln())
	}

	if len(aliceReceived) != aliceExpectedCount {
		t.Errorf("Expected %d messages from Alice, got %d", aliceExpectedCount, len(aliceReceived))
	}
	if len(bobReceived) != bobExpectedCount {
		t.Errorf("Expected %d messages from Bob, got %d", bobExpectedCount, len(bobReceived))
	}
	if len(charlieReceived) != charlieExpectedCount {
		t.Errorf("Expected %d messages from Charlie, got %d", charlieExpectedCount, len(charlieReceived))
	}

	for _, s := range aliceReceived {
		if !contains(expectedFromBob, s) && !contains(expectedFromCharlie, s) {
			t.Errorf("Unexpected message from Alice: %s", s)
		}
	}
	for _, s := range bobReceived {
		if !contains(expectedFromAlice, s) && !contains(expectedFromCharlie, s) {
			t.Errorf("Unexpected message from Bob: %s", s)
		}
	}
	for _, s := range charlieReceived {
		if !contains(expectedFromAlice, s) && !contains(expectedFromBob, s) {
			t.Errorf("Unexpected message from Charlie: %s", s)
		}
	}
}

/** Test : Servers closes gracefully */
func TestServerClose(t *testing.T) {
	_, s1, _ := createServer(t, "./server1.log", "Alice", addrs[0], []transport.Address{addrs[1], addrs[2]}, false, 0)
	_, s2, _ := createServer(t, "./server2.log", "Bob", addrs[1], []transport.Address{addrs[0], addrs[2]}, false, 0)
	_, s3, _ := createServer(t, "./server3.log", "Charlie", addrs[2], []transport.Address{addrs[0], addrs[1]}, false, 0)

	s1.Close()
	s2.Close()
	s3.Close()
	time.Sleep(5 * time.Second)
}

func TestServerCrashAndRecovery(t *testing.T) {
	s1Input := "Hi, I'm Alice."

	slowdownMs := uint32(5)

	// addresses := []string{"127.0.0.1:5000", "127.0.0.1:5001"}

	// Create two connected servers
	i1, _, _ := createServer(t, "./server1.log", "Alice", addrs[0], []transport.Address{addrs[1]}, true, 0)
	i2, s2, ni2 := createServer(t, "./server2.log", "Bob", addrs[1], []transport.Address{addrs[0]}, true, slowdownMs)
	logger2 := s2.logger

	// Start a goroutine that closes Bob every x intervals, with x increasing from 0.1s to 10s
	go func() {
		min := 1 * time.Millisecond
		max := 1 * time.Second
		step := 1 * time.Millisecond

		for i := min; i < max; i += step {
			time.Sleep(i)
			fmt.Println("Crashing Bob after " + i.String() + " of uptime")
			s2.Close()
			s2 = newServer(i2, logger2, false, addrs[1], s2.user, []transport.Address{addrs[0]}, true, ni2, slowdownMs)
		}
	}()

	i1.SimulateNextInputLine(s1Input)
	expected := "[127.0.0.1:5001 received: " + s1Input + "]\n"

	responseChan := make(chan string)
	go func() {
		s := i1.InterceptNextPrintln()
		responseChan <- s
	}()

	select {
	case <-time.After(30 * time.Second):
		t.Errorf("Timeout: No response received within 20 seconds")
	case s := <-responseChan:
		if s != expected {
			t.Errorf("Expected %s, got %s", expected, s)
		}
	}
}
