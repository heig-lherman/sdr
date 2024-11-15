package server

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/mutex"
	"chatsapp/internal/server/dispatcher"
	"chatsapp/internal/transport"
	"chatsapp/internal/transport/tcp"
	"chatsapp/internal/utils/ioUtils"
	"fmt"
	"strconv"
	"time"
)

// Represents a server in ChatsApp's distributed system.
type Server struct {
	ioStream     ioUtils.IOStream
	logger       *logging.Logger
	self         transport.Address
	neighbors    []transport.Address
	user         Username
	printReadAck bool

	network    transport.NetworkInterface
	dispatcher dispatcher.Dispatcher
	mutex      mutex.Mutex

	closeNotifier chan bool

	debug       bool
	slowdownMs  uint32
	ownsNetwork bool
}

/*
Constructs and returns a new server instance.
  - config: The configuration for the server.
  - networkInterface: The network interface to use for communication.
*/
func NewServer(config *ServerConfig) *Server {
	logFile := logging.NewLogFile(fmt.Sprintf("%s/server-%s.log", config.LogPath, strconv.Itoa(int(config.Addr.Port))))

	ioStream := ioUtils.NewStdStream()

	log := logging.NewLogger(ioStream, logFile, fmt.Sprintf("srv(%v<->..)", config.Addr.Port), !config.Debug)
	log.Info("Starting server ", config.Addr.Port)

	networkInterface := tcp.NewTCP(config.Addr, log.WithPostfix("tcp"))

	s := newServer(ioUtils.NewStdStream(), log, config.Debug, config.Addr, config.User, config.Neighbors, config.PrintReadAck, networkInterface, config.SlowdownMs)
	s.ownsNetwork = true

	return s
}

/*
Constructs a new server instance from detailed parameters. This is intended to be used directly only by the tests.
  - ioStream: The input/output stream to use for user interaction.
  - log: The logger instance to use.
  - debug: Whether to run in debug mode.
  - selfAddr: The address of the server.
  - clientCommStrat: The strategy to use for communication with clients.
  - neighbors: The addresses of the server's neighbors, ordered according to their order in the ring.
  - printReadAck: Whether to print read acknoledgements.
  - networkInterface: The network interface to use for communication.
  - slowdown: The amount of time to sleep after sending a message.
*/
func newServer(ioStream ioUtils.IOStream, log *logging.Logger, debug bool, selfAddr transport.Address, username Username, neighbors []transport.Address, printReadAck bool, networkInterface transport.NetworkInterface, slowdown uint32) *Server {
	server := Server{
		ioStream:      ioStream,
		debug:         debug,
		logger:        log,
		self:          selfAddr,
		neighbors:     neighbors,
		network:       networkInterface,
		printReadAck:  printReadAck,
		slowdownMs:    slowdown,
		user:          username,
		closeNotifier: make(chan bool),
		ownsNetwork:   false,
	}

	server.dispatcher = dispatcher.NewDispatcher(log.WithPostfix("disp"), selfAddr, networkInterface)
	server.dispatcher.Register(ChatMessage{}, server.handleDispatchedChatMessage)

	server.initMutex()

	return &server
}

func (s *Server) initMutex() {
	var pid = mutex.Pid(s.self.String())
	var neighborsPids []mutex.Pid
	for _, addr := range s.neighbors {
		neighborsPids = append(neighborsPids, mutex.Pid(addr.String()))
	}

	mutexToNet := make(chan mutex.OutgoingMessage)
	netToMutex := make(chan mutex.Message)
	s.mutex = mutex.NewLamportMutex(
		s.logger.WithPostfix("mutex"),
		mutex.NetWrapper{IntoNet: mutexToNet, FromNet: netToMutex},
		pid,
		neighborsPids,
	)

	s.dispatcher.Register(mutex.Message{}, s.handleDispatchedMutexMessage(netToMutex))
	go s.handleMutexToNetworkMessages(mutexToNet)
}

// Handles chat messages dispatched by the [dispatcher.Dispatcher] instance. Essentially only forwards them to clients.
func (s *Server) handleDispatchedChatMessage(msg dispatcher.Message, from transport.Address) {
	s.slowSelfDown()

	chatMsg, ok := msg.(ChatMessage)
	if !ok {
		s.logger.Error("Received message of unknown type ; ingoring")
		return
	}

	s.logger.Info("Chat message from", from, ":", chatMsg.User, ":", chatMsg.Content)

	s.ioStream.Println(fmt.Sprintf("%s: %s", chatMsg.User, chatMsg.Content))
}

// Handles mutex messages dispatched by the [dispatcher.Dispatcher] instance.
// Essentially only forwards them to the mutex.
func (s *Server) handleDispatchedMutexMessage(netToMutex chan mutex.Message) dispatcher.ProtocolHandler {
	return func(msg dispatcher.Message, source transport.Address) {
		s.slowSelfDown()

		mutexMsg, ok := msg.(mutex.Message)
		if !ok {
			s.logger.Error("Received message of unknown type; ingoring")
			return
		}

		s.logger.Infof("Mutex message from %v : %#v", source, mutexMsg)
		netToMutex <- mutexMsg
	}
}

// Handles messages from the mutex to be sent to the network, ran as a goroutine.
func (s *Server) handleMutexToNetworkMessages(intoNet chan mutex.OutgoingMessage) {
	for {
		select {
		case msg := <-intoNet:
			s.logger.Infof("Sending mutex message to network: %#v", msg)
			addr, err := transport.NewAddress(string(msg.Destination))
			if err != nil {
				s.logger.Errorf("Failed to create address from PID %v: %v", msg.Destination, err)
				continue
			}

			s.dispatcher.Send(msg.Message, addr)
		case <-s.closeNotifier:
			s.logger.Warn("Mutex network stopped due to close request")
			return
		}
	}
}

// Launches the server's main loop.
func (s *Server) Start() {
	s.logger.Info("Starting server")

	// Listening to user inputs
	readLines := make(chan string)
	go func() {
		for {
			s.logger.Info("Waiting for user input")
			nextLine, err := s.ioStream.ReadLine()
			if err != nil {
				s.logger.Errorf("Error reading line from user: %s", err)
				continue
			}
			readLines <- nextLine
			if s.isClosed() {
				s.logger.Warnf("Server stops listening for user input")
				return
			}
		}
	}()

	for {
		select {
		case line := <-readLines:
			s.logger.Info("New input from user:", line)
			s.broadcast(s.user, line)
		case <-s.closeNotifier:
			s.logger.Warn("Server stopped due to close")
			return
		}
	}
}

// May be called to sleep for the configured duration. Used to simulate slow systems in tests.
func (s *Server) slowSelfDown() {
	if s.slowdownMs > 0 {
		time.Sleep(time.Duration(s.slowdownMs) * time.Millisecond)
	}
}

// Constructs the string intended for printing a message receipt acknowledgement.
func (s *Server) constructMsgReceiptAckString(from transport.Address, message string) string {
	return fmt.Sprintf("[%s received: %s]", from, message)
}

// Broadcasts a message to all neighbor servers.
func (s *Server) broadcast(from Username, text string) {
	s.logger.Info("Broadcasting to neighbors:", s.neighbors)

	if s.isClosed() {
		s.logger.Warn("Server is closed. Ignoring broadcast")
		return
	}

	// Acquire mutex before broadcasting to ensure message total order
	release, err := s.mutex.Request()
	if err != nil {
		s.logger.Errorf("Failed to acquire mutex: %v", err)
		return
	}
	defer release()

	// For each neighbor,send the message
	message := ChatMessage{User: Username(from), Content: text}

	for _, addr := range s.neighbors {
		if addr == s.self {
			s.logger.Warnf("There is a neighbor for self: %v. This shouldn't happen. Ignoring it upon broadcast", addr)
			continue
		}
		s.slowSelfDown()

		s.logger.Info("Sending message to", addr)
		s.dispatcher.Send(message, addr)
		if s.printReadAck {
			s.ioStream.Println(s.constructMsgReceiptAckString(addr, text))
		}
		if s.isClosed() {
			s.logger.Warn("Broadcast stopped due to close request")
			return
		}
	}
}

func (s *Server) isClosed() bool {
	select {
	case <-s.closeNotifier:
		return true
	default:
		return false
	}
}

func (s *Server) Close() {
	s.logger.Info("Closing server")
	if s.isClosed() {
		s.logger.Warn("Server already closed")
		return
	}
	close(s.closeNotifier)

	s.dispatcher.Close()

	if s.ownsNetwork {
		// If server was created by the test, it doesn't own the network and shouldn't close it.
		s.network.Close()
	}

}
