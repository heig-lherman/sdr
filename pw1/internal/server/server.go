package server

import (
	"bytes"
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/transport/tcp"
	"chatsapp/internal/utils/ioUtils"
	"encoding/gob"
	"fmt"
	"strconv"
	"time"
)

// Represents a server in ChatsApp's distributed system.
type Server struct {
	ioStream  ioUtils.IOStream
	logger    *logging.Logger
	self      transport.Address
	neighbors []transport.Address

	network transport.NetworkInterface
	user    Username

	debug        bool
	printReadAck bool
	slowdownMs   uint32

	ownsNetwork bool
	closeChan   chan struct{}
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

	networkInterface := tcp.NewTCP(config.Addr, config.Neighbors, log.WithPostfix("tcp"))

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
		ioStream:     ioStream,
		debug:        debug,
		logger:       log,
		self:         selfAddr,
		neighbors:    neighbors,
		network:      networkInterface,
		printReadAck: printReadAck,
		slowdownMs:   slowdown,
		user:         username,

		ownsNetwork: false,
		closeChan:   make(chan struct{}),
	}

	networkInterface.RegisterHandler(&server)

	return &server
}

func (s *Server) isClosed() bool {
	select {
	case <-s.closeChan:
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

	close(s.closeChan)
	if s.ownsNetwork {
		// If server was created by the test, it doesn't own the network and shouldn't close it.
		s.network.Close()
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
			if s.isClosed() {
				s.logger.Warn("Server is closed. Ignoring user input")
				return
			}
			readLines <- nextLine
		}
	}()

	for {
		select {
		case line := <-readLines:
			s.logger.Info("New input from user:", line)
			s.broadcast(s.user, line)
		case <-s.closeChan:
			s.logger.Warn("Server stopped due to close request")
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

// Handles chat messages received from neighbors.
func (s *Server) HandleNetworkMessage(m *transport.Message) (wasHandled bool) {
	wasHandled = true

	s.slowSelfDown()

	chatMsg, err := decodeMessage(m.Payload)

	if err != nil {
		s.logger.Errorf("Error decoding message: %s. Ignoring", err)
		return false
	}

	s.logger.Infof("Received chat message from %s (%s): %s", m.Source, chatMsg.User, chatMsg.Content)
	s.ioStream.Println(fmt.Sprintf("%s: %s", chatMsg.User, chatMsg.Content))

	return
}

// Broadcasts a message to all neighbor servers.
func (s *Server) broadcast(from Username, text string) {
	s.logger.Info("Broadcasting to neighbors:", s.neighbors)

	if s.isClosed() {
		s.logger.Warn("Server is closed. Ignoring broadcast request")
		return
	}

	// For each neighbor,send the message
	message := ChatMessage{User: Username(from), Content: text}

	payload, err := encodeMessage(message)
	if err != nil {
		s.logger.Errorf("Error encoding message: %s. Ignoring", err)
		return
	}

	for _, addr := range s.neighbors {
		if addr == s.self {
			s.logger.Errorf("There is a neighbor that is the same as self: %v. Ignoring it upon broadcast", addr)
			continue
		}
		s.slowSelfDown()

		s.logger.Info("Sending message to", addr)
		err := s.network.Send(addr, payload)
		if err != nil {
			s.logger.Errorf("Error sending message to %v: %s", addr, err)
			continue
		}

		if s.printReadAck {
			s.ioStream.Println(fmt.Sprintf("[%v received: %s]", addr, text))
		}
	}
}

func decodeMessage(payload []byte) (ChatMessage, error) {
	decodedMsg := ChatMessage{}
	decoder := gob.NewDecoder(bytes.NewReader(payload))
	err := decoder.Decode(&decodedMsg)
	return decodedMsg, err
}

func encodeMessage(msg ChatMessage) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(msg)
	return buf.Bytes(), err
}
