package server

import (
	"chatsapp/internal/client"
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/transport/tcp"
	"chatsapp/internal/utils/ioUtils"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

var addrs = []transport.Address{
	{IP: "127.0.0.1", Port: 5000},
	{IP: "127.0.0.1", Port: 5001},
	{IP: "127.0.0.1", Port: 5002},
}

func localClient(username string) clientCommStrategy {
	return newLocalClientCommStrategy(Username(username))
}

func remoteClient() clientCommStrategy {
	return newRemoteClientCommStrategy()
}

func createServer(t *testing.T, logFileName string, clientComm clientCommStrategy, addr transport.Address, neighbors []transport.Address, printReadAck bool, slowdownMs uint32) (ioStream ioUtils.MockIOStream, server *Server, networkInterface transport.NetworkInterface) {
	debug := true
	ioStream = ioUtils.NewMockReader()
	logFile := logging.NewLogFile(logFileName)
	log := logging.NewLogger(ioStream, logFile, strconv.Itoa(int(addr.Port)), false)
	// networkInterface = transport.NewUDPInterface(addr, log)
	networkInterface = tcp.NewTCP(addr, log.WithPostfix("tcp").WithLogLevel(logging.WARN))
	containsSelf := false
	for _, neighbor := range neighbors {
		if neighbor == addr {
			containsSelf = true
			break
		}
	}
	if !containsSelf {
		neighbors = append(neighbors, addr)
	}
	log.Infof("Creating server at %s with neighbors %v", addr, neighbors)
	server = newServer(ioStream, log, debug, addr, clientComm, neighbors, printReadAck, networkInterface, slowdownMs)
	go server.Start()
	t.Cleanup(func() {
		server.Close()
		networkInterface.Close()
		fmt.Println("Closing server")
	})
	return
}

func createClient(t *testing.T, username string, serverAddr transport.Address, selfAddr transport.Address) (c client.Client, ni transport.NetworkInterface, ioStream ioUtils.MockIOStream) {
	ioStream = ioUtils.NewMockReader()
	log := logging.NewStdLogger("cli-" + username)
	ni = tcp.NewTCP(selfAddr, log.WithPostfix("tcp").WithLogLevel(logging.WARN))
	c = client.NewClient(log, serverAddr, selfAddr, client.Username(username), ni, ioStream)
	go c.Run()
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

	i1, _, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1]}, true, 0)
	i2, _, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0]}, true, 0)

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

	i1, _, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1]}, true, 0)
	i2, _, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0]}, true, 0)

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

	i1, _, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1]}, false, 0)
	i2, _, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0]}, false, 0)

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

func TestServerSpamBidirectionalMessages(t *testing.T) {
	numMessages := 1000

	s1Inputs := make([]string, numMessages)
	s2Inputs := make([]string, numMessages)

	for i := 0; i < numMessages; i++ {
		s1Inputs[i] = "Alice message " + strconv.Itoa(i)
		s2Inputs[i] = "Bob message " + strconv.Itoa(i)
	}

	i1, _, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1]}, false, 0)
	i2, _, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0]}, false, 0)

	go func() {
		for _, input := range s1Inputs {
			i1.SimulateNextInputLine(input)
		}
	}()

	go func() {
		for _, input := range s2Inputs {
			i2.SimulateNextInputLine(input)
		}
	}()

	for i := 0; i < numMessages; i++ {
		expectedAlice := "Alice: " + s1Inputs[i] + "\n"
		expectedBob := "Bob: " + s2Inputs[i] + "\n"
		s := i2.InterceptNextPrintln()
		if s != expectedAlice {
			t.Errorf("Expected %s, got %s", expectedAlice, s)
		}
		s = i1.InterceptNextPrintln()
		if s != expectedBob {
			t.Errorf("Expected %s, got %s", expectedBob, s)
		}
	}
}

/** Test : Servers send messages one after the other */
func TestServerSendsMessage(t *testing.T) {
	s1Inputs := []string{
		"Hello, I'm Alice!",
	}

	s2Inputs := []string{
		"Hi, I'm Bob.",
		"Nice to meet you, Alice!",
	}

	i1, _, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1]}, true, 0)
	i2, _, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0]}, true, 0)

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
	i1, _, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1], addrs[2]}, false, 0)

	i2, _, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0], addrs[2]}, false, 0)

	i3, _, _ := createServer(t, "./server3.log", localClient("Charlie"), addrs[2], []transport.Address{addrs[0], addrs[1]}, false, 0)

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

/** Test : Servers close gracefully */
func TestServerClose(t *testing.T) {
	_, s1, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1], addrs[2]}, false, 0)
	_, s2, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0], addrs[2]}, false, 0)
	_, s3, _ := createServer(t, "./server3.log", localClient("Charlie"), addrs[2], []transport.Address{addrs[0], addrs[1]}, false, 0)

	s1.Close()
	s2.Close()
	s3.Close()

	time.Sleep(2 * time.Second)
}

func TestGlobalOrdering(t *testing.T) {
	msgCountEach := 100

	aliceInputs := make([]string, msgCountEach)
	bobInputs := make([]string, msgCountEach)

	for i := 0; i < msgCountEach; i++ {
		aliceInputs[i] = fmt.Sprintf("Alice-%d", i)
		bobInputs[i] = fmt.Sprintf("Bob-%d", i)
	}

	// Create 4 connected servers
	addrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5000},
		{IP: "127.0.0.1", Port: 5001},
		{IP: "127.0.0.1", Port: 5002},
		{IP: "127.0.0.1", Port: 5003},
	}

	i1, s1, _ := createServer(t, "./server1.log", localClient("Alice"), addrs[0], []transport.Address{addrs[1], addrs[2], addrs[3]}, false, 0)
	i2, s2, _ := createServer(t, "./server2.log", localClient("Bob"), addrs[1], []transport.Address{addrs[0], addrs[2], addrs[3]}, false, 0)
	i3, s3, _ := createServer(t, "./server3.log", localClient("Charlie"), addrs[2], []transport.Address{addrs[0], addrs[1], addrs[3]}, false, 0)
	i4, s4, _ := createServer(t, "./server4.log", localClient("David"), addrs[3], []transport.Address{addrs[0], addrs[1], addrs[2]}, false, 0)

	// Send Alice's and Bob's messages
	go func() {
		for _, input := range aliceInputs {
			i1.SimulateNextInputLine(input)
		}
	}()
	go func() {
		for _, input := range bobInputs {
			i2.SimulateNextInputLine(input)
		}
	}()

	// Receive messages on Charlie and David
	charlieReceived := []string{}
	davidReceived := []string{}

	for i := 0; i < msgCountEach*2; i++ {
		charlieReceived = append(charlieReceived, i3.InterceptNextPrintln())
		davidReceived = append(davidReceived, i4.InterceptNextPrintln())
	}

	// Check that the messages are in the same order
	for i := 0; i < msgCountEach*2; i++ {
		if charlieReceived[i] != davidReceived[i] {
			t.Errorf("Unordered messages: message #%d received by Charlie was %s; David's was %s", i+1, charlieReceived[i], davidReceived[i])
		}
	}

	s1.Close()
	s2.Close()
	s3.Close()
	s4.Close()

	time.Sleep(2 * time.Second)
}

func TestUnidirectionalWithClients(t *testing.T) {
	aliceInputs := []string{
		"Hello, I'm Alice!",
		"Hi Bob!",
		"Hi Bob again!",
	}

	// Create 4 connected servers
	serverAddrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5050},
		{IP: "127.0.0.1", Port: 5051},
	}

	_, s1, _ := createServer(t, "./server1.log", remoteClient(), serverAddrs[0], []transport.Address{serverAddrs[1]}, false, 0)
	_, s2, _ := createServer(t, "./server2.log", remoteClient(), serverAddrs[1], []transport.Address{serverAddrs[0]}, false, 0)

	_, nc2, c2Stream := createClient(t, "Bob", serverAddrs[1], transport.Address{IP: "127.0.0.1", Port: 7001})
	time.Sleep(1 * time.Second)
	_, nc1, c1Stream := createClient(t, "Alice", serverAddrs[0], transport.Address{IP: "127.0.0.1", Port: 7000})

	for _, input := range aliceInputs {
		c1Stream.SimulateNextInputLine(input)
		l := c2Stream.InterceptNextPrintln()
		expected := "Alice: " + input + "\n"
		if l != expected {
			t.Errorf("Expected %s, got %s", expected, l)
		}
	}

	time.Sleep(1 * time.Second)

	s1.Close()
	s2.Close()

	nc1.Close()
	nc2.Close()

	time.Sleep(2 * time.Second)
}

func TestTwoClientsOnSameServer(t *testing.T) {
	// In this test, 4 users connect on 2 servers, so that each server has 2 users.
	// One user then sends messages; all 3 other users should receive them, especially the one sharing the server with the sender.

	inputs := []string{
		"Hello there!",
		"Nice to meet you all",
	}

	serverAddrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5050},
		{IP: "127.0.0.1", Port: 5051},
	}

	clientAddrs := []transport.Address{
		{IP: "127.0.0.1", Port: 6050},
		{IP: "127.0.0.1", Port: 6051},
		{IP: "127.0.0.1", Port: 6052},
		{IP: "127.0.0.1", Port: 6053},
	}

	_, s1, _ := createServer(t, "./server1.log", remoteClient(), serverAddrs[0], []transport.Address{serverAddrs[1]}, false, 0)
	_, s2, _ := createServer(t, "./server2.log", remoteClient(), serverAddrs[1], []transport.Address{serverAddrs[0]}, false, 0)

	_, nc1, c1Stream := createClient(t, "Alice", serverAddrs[0], clientAddrs[0])
	time.Sleep(500 * time.Millisecond)
	_, nc2, c2Stream := createClient(t, "Bob", serverAddrs[0], clientAddrs[1])
	time.Sleep(500 * time.Millisecond)
	_, nc3, c3Stream := createClient(t, "Charlie", serverAddrs[1], clientAddrs[2])
	time.Sleep(500 * time.Millisecond)
	_, nc4, c4Stream := createClient(t, "David", serverAddrs[1], clientAddrs[3])
	time.Sleep(500 * time.Millisecond)

	for _, input := range inputs {
		c1Stream.SimulateNextInputLine(input)
		l := c2Stream.InterceptNextPrintln()
		expected := "Alice: " + input + "\n"
		if l != expected {
			t.Errorf("Expected %s, got %s", expected, l)
		}
		l = c3Stream.InterceptNextPrintln()
		if l != expected {
			t.Errorf("Expected %s, got %s", expected, l)
		}
		l = c4Stream.InterceptNextPrintln()
		if l != expected {
			t.Errorf("Expected %s, got %s", expected, l)
		}
	}

	time.Sleep(1 * time.Second)

	s1.Close()
	s2.Close()
	nc1.Close()
	nc2.Close()
	nc3.Close()
	nc4.Close()

	time.Sleep(2 * time.Second)
}

func TestConcurrentConnection(t *testing.T) {
	// 2 servers, 2 clients connecting to the same server

	serverAddrs := []transport.Address{
		{IP: "127.0.0.1", Port: 5050},
		{IP: "127.0.0.1", Port: 5051},
	}

	clientAddrs := []transport.Address{
		{IP: "127.0.0.1", Port: 6050},
		{IP: "127.0.0.1", Port: 6051},
	}

	_, s1, _ := createServer(t, "./server1.log", remoteClient(), serverAddrs[0], []transport.Address{serverAddrs[1]}, false, 0)
	_, s2, _ := createServer(t, "./server2.log", remoteClient(), serverAddrs[1], []transport.Address{serverAddrs[0]}, false, 0)

	_, nc1, c1Stream := createClient(t, "Alice", serverAddrs[0], clientAddrs[0])
	_, nc2, c2Stream := createClient(t, "Bob", serverAddrs[0], clientAddrs[1])

	time.Sleep(1 * time.Second)

	input := "Hello there!"
	c1Stream.SimulateNextInputLine(input)
	output := c2Stream.InterceptNextPrintln()
	expected := "Alice: " + input + "\n"
	if output != expected {
		t.Errorf("Expected %s, got %s", expected, output)
	}

	time.Sleep(1 * time.Second)

	s1.Close()
	s2.Close()

	nc1.Close()
	nc2.Close()

	time.Sleep(2 * time.Second)
}

func TestStress(t *testing.T) {
	// 3 servers, 6 clients, they connect all at the same time and 4 of them send messages.
	// The remaining two clients should receive all messages in the same order.

	serverCount := 3
	clientCount := 10
	msgsPerClient := 100

	serverAddrs := make([]transport.Address, serverCount)
	for i := 0; i < serverCount; i++ {
		serverAddrs[i] = transport.Address{IP: "127.0.0.1", Port: 5050 + uint16(i)}
	}

	servers := make([]*Server, serverCount)
	for i := 0; i < serverCount; i++ {
		_, servers[i], _ = createServer(t, "./server"+strconv.Itoa(i)+".log", remoteClient(), serverAddrs[i], serverAddrs, false, 0)
	}

	clients := make([]ioUtils.MockIOStream, clientCount)
	clientNetworks := make([]transport.NetworkInterface, clientCount)
	for i := 0; i < clientCount; i++ {
		addr := transport.Address{IP: "127.0.0.1", Port: 6050 + uint16(i)}
		serverAddr := serverAddrs[rand.Intn(len(serverAddrs))]
		_, clientNetworks[i], clients[i] = createClient(t, "User"+strconv.Itoa(i), serverAddr, addr)
	}

	time.Sleep(3 * time.Second)

	for i := 0; i < clientCount-2; i++ {
		go func(i int) {
			for j := 0; j < msgsPerClient; j++ {
				clients[i].SimulateNextInputLine("User" + strconv.Itoa(i) + " message " + strconv.Itoa(j))
			}
		}(i)
	}

	time.Sleep(3 * time.Second)

	for j := 0; j < msgsPerClient*(clientCount-2); j++ {
		msg0 := clients[clientCount-2].InterceptNextPrintln()
		msg1 := clients[clientCount-1].InterceptNextPrintln()

		if msg0 != msg1 {
			t.Errorf("Mismatched messages: '%s' vs '%s'", msg0, msg1)
		}
	}

	for i := 0; i < serverCount; i++ {
		servers[i].Close()
	}

	for i := 0; i < clientCount; i++ {
		clientNetworks[i].Close()
	}

	time.Sleep(2 * time.Second)
}
