package server

import (
	"chatsapp/internal/common"
	"chatsapp/internal/election"
	"chatsapp/internal/logging"
	"chatsapp/internal/server/dispatcher"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
)

/*
Responsible for managing remote clients connected to this client. This includes:
- Accepting and redirecting new connections
- Handling client disconnections
- Receiving messages from clients
- Broadcasting messages to all clients
*/
type ClientsManager interface {
	// Broadcasts a message to all connected clients
	Broadcast(msg common.ChatMessage)
	// Returns the next message received from a client. Blocks until a message is received. If the manager is closed, returns a [ClientsManagerClosedError].
	ReceiveMessage() (common.ChatMessage, error)
}

type ClientsManagerClosedError struct{}

func (ClientsManagerClosedError) Error() string {
	return "Clients manager closed"
}

// Represents a connection to a single client, i.e. the address of the client and the username of user on whose behalf the client is communicating.
type clientConnection struct {
	source transport.Address
	user   common.Username
}

// Represents a message received from a client.
type incomingMessage struct {
	common.Message
	source transport.Address
}

// Implements the [ClientsManager] interface.
type clientsManager struct {
	logger *logging.Logger

	self transport.Address

	elector    election.Elector
	dispatcher dispatcher.Dispatcher

	incomingMessages *utils.BufferedChan[incomingMessage]

	broadcastReqs chan common.ChatMessage

	validatedMsgs *utils.BufferedChan[common.ChatMessage]
}

/*
Constructs and returns a new [ClientsManager].
  - logger: The logger to use for logging messages.
  - self: The address of the current client.
  - elector: The elector to use for leader election.
  - network: The network interface to use for sending and receiving messages to/from the clients.

Note that communication with the clients does not go through a dispatcher, but through teh network interface directly, for simplicity.
*/
func NewClientManager(logger *logging.Logger, self transport.Address, elector election.Elector, disp dispatcher.Dispatcher) ClientsManager {
	m := clientsManager{
		logger:           logger,
		self:             self,
		elector:          elector,
		dispatcher:       disp,
		incomingMessages: utils.NewBufferedChan[incomingMessage](),
		broadcastReqs:    make(chan common.ChatMessage),
		validatedMsgs:    utils.NewBufferedChan[common.ChatMessage](),
	}

	common.RegisterAllToGob()
	disp.Register(common.ConnRequestMessage{}, m.handleDispatchedMessage)
	disp.Register(common.ChatMessage{}, m.handleDispatchedMessage)
	disp.Register(common.ConnClose{}, m.handleDispatchedMessage)
	go m.handleClients()

	return &m
}

func (m *clientsManager) handleDispatchedMessage(msg dispatcher.Message, source Address) {
	m.incomingMessages.Inlet() <- incomingMessage{
		Message: msg,
		source:  source,
	}
}

func (m *clientsManager) Broadcast(msg common.ChatMessage) {
	m.broadcastReqs <- msg
}

func (m *clientsManager) ReceiveMessage() (common.ChatMessage, error) {
	msg, ok := <-m.validatedMsgs.Outlet()
	if !ok {
		return common.ChatMessage{}, ClientsManagerClosedError{}
	}
	return msg, nil
}

// Main goroutine that handles the clients.
//
// The clientsManager maintains a map of all connected clients. Because clients may connect and disconnect dynamically over time, this is state that must be handled by a single goroutine to avoid concurrent access. This is that goroutine.
//
// It listens for incoming messages from clients.
//   - If a client connection request, it responds with the current leader and adds the client to the map if it itself the leader,
//   - If a chat message, it ensures it comes from the expected user and forwards it to be received by the [clientsManager.ReceiveMessage] method,
//   - If a connection close message, it removes the client from the map.
//
// It also listens for requests from the server to broadcast a message to all clients.
func (m *clientsManager) handleClients() {
	clients := make(map[transport.Address]clientConnection)

	for {
		select {
		case incomingMsg := <-m.incomingMessages.Outlet():
			msg := incomingMsg.Message
			source := incomingMsg.source

			switch clientMsg := msg.(type) {
			case common.ConnRequestMessage:
				m.logger.Infof("Received conn req from %s (%s)", source, clientMsg.User)

				if _, ok := clients[source]; ok {
					m.logger.Warn("Client already connected: ", source)
					continue
				}

				leader := m.elector.GetLeader()
				m.logger.Infof("Responding to conn req from %s with leader %s", source, leader)
				m.dispatcher.Send(common.ConnResponseMessage{
					Leader: leader,
				}, source)

				if leader == m.self {
					clients[source] = clientConnection{source: source, user: clientMsg.User}
					m.elector.UpdateAbility(-len(clients))
				}
			case common.ChatMessage:
				if client, ok := clients[source]; !ok {
					m.logger.Warnf("Received chat msg from unconnected client %s. Ignoring", source)
					continue
				} else if client.user != clientMsg.User {
					m.logger.Warnf("Received chat msg from %s with username %s while expecting %s. Ignoring", source, clientMsg.User, client.user)
					continue
				}
				m.logger.Infof("Received msg from %s: %s", source, clientMsg)
				m.validatedMsgs.Inlet() <- clientMsg
			case common.ConnClose:
				if _, ok := clients[source]; !ok {
					m.logger.Warn("Received close request from unconnected client; ignoring: ", source)
					continue
				}
				m.logger.Info("Client disconnected: ", source)
				delete(clients, source)
				m.elector.UpdateAbility(-len(clients))
			}
		case msg := <-m.broadcastReqs:
			for addr, client := range clients {
				if client.user != msg.User {
					m.dispatcher.Send(msg, addr)
				}
			}
		}
	}
}
