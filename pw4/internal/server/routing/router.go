package routing

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/messages"
	"chatsapp/internal/server/pulsing"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"fmt"
)

// Router is an interface for sending messages to any node in the network, or to broadcast to all nodes in the network, even if the network is not fully connected.
type Router interface {
	// Broadcast to all processes in the network, blocking until all have recived, returning their addresses.
	Broadcast(msg messages.Message) (receivers []transport.Address, err error)

	// Send to a given process in the network, blocking until the message has been sent onto the network. Does not wait for an acknowledgement from the destination.
	Send(msg messages.Message, dest transport.Address) error

	// Return a channel onto which the router will send the next message received from any other process in the network
	ReceivedMessageChan() <-chan receivedMessage
}

type routedSendReq struct {
	RoutedMessage
	response chan error
}

// receivedMessage is a message received by the router, along with the source address
type receivedMessage = messages.Sourced[messages.Message]

type router struct {
	log  *logging.Logger
	self transport.Address

	table  *routingTableHandler
	pulsar pulsing.Pulsar[messages.Message]

	routerToNet      chan<- messages.Destined[RoutedMessage]
	sendRequests     chan<- routedSendReq
	receivedMessages chan<- receivedMessage

	// Only here for the getter on it, should not be used within the router
	messageOutput <-chan receivedMessage
}

// NewRouter creates a new router instance.
func NewRouter(
	logger *logging.Logger,
	self transport.Address,
	neighbors []transport.Address,
	routerToNet chan<- messages.Destined[RoutedMessage],
	netToRouter <-chan messages.Sourced[RoutedMessage],
	pulsarBuilder pulsing.Builder[messages.Message],
) Router {
	initialTable := make(routingTable)
	for _, neighbor := range neighbors {
		initialTable[neighbor] = neighbor
	}

	return newRouter(logger, self, routerToNet, netToRouter, initialTable, pulsarBuilder)
}

func newRouter(
	logger *logging.Logger,
	self transport.Address,
	routerToNet chan<- messages.Destined[RoutedMessage],
	netToRouter <-chan messages.Sourced[RoutedMessage],
	table routingTable,
	pulsarBuilder pulsing.Builder[messages.Message],
) *router {
	BroadcastRequest{}.RegisterToGob()
	BroadcastResponse{}.RegisterToGob()
	ExplorationRequest{}.RegisterToGob()
	ExplorationResponse{}.RegisterToGob()

	sendRequests := utils.NewBufferedChan[routedSendReq]()
	receivedMessages := utils.NewBufferedChan[receivedMessage]()

	r := &router{
		log:              logger,
		self:             self,
		routerToNet:      routerToNet,
		table:            newRoutingTableHandler(logger, table),
		sendRequests:     sendRequests.Inlet(),
		receivedMessages: receivedMessages.Inlet(),
		messageOutput:    receivedMessages.Outlet(),
	}

	r.pulsar = pulsarBuilder.Build(
		logger.WithPostfix("pulsar"),
		r.handleIncomingPulse,
		r.handleIncomingEcho,
	)

	go r.handleSendRequests(sendRequests.Outlet())
	go r.handleIncomingMessages(netToRouter)

	return r
}

func (r *router) handleIncomingPulse(
	_ pulsing.PulseID,
	received messages.Message,
	from transport.Address,
) (messages.Message, error) {
	r.log.Infof("Received pulse from %v: %v", from, received)
	switch msg := received.(type) {
	case BroadcastRequest:
		r.receivedMessages <- receivedMessage{Message: msg.Msg, From: from}
	}

	return received, nil
}

func (r *router) handleIncomingEcho(
	_ pulsing.PulseID,
	pulse messages.Message,
	echoes []pulsing.SourcedMessage[messages.Message],
) (messages.Message, error) {
	r.log.Infof("Aggregating %d echoes for pulse %v", len(echoes), pulse)
	switch pulse.(type) {
	case BroadcastRequest:
		receivers := []transport.Address{r.self}
		for _, echo := range echoes {
			receivers = append(receivers, echo.From)
		}

		return BroadcastResponse{Receivers: receivers}, nil

	case ExplorationRequest:
		table := r.joinRoutingTables(echoes)
		r.log.Infof("Aggregated routing tables: %v", table)
		r.table.UpdateTable(table)
		return ExplorationResponse{RoutingTable: table}, nil
	}

	return pulse, nil
}

func (r *router) handleIncomingMessages(
	netToRouter <-chan messages.Sourced[RoutedMessage],
) {
	for msg := range netToRouter {
		if msg.Message.To == r.self {
			r.log.Infof("Delivering local message from %v", msg.From)
			r.receivedMessages <- receivedMessage{
				Message: msg.Message.Msg,
				From:    msg.Message.From,
			}
		} else {
			r.log.Infof("Routing message from %v to %v", msg.From, msg.Message.To)
			res := make(chan error)
			r.sendRequests <- routedSendReq{RoutedMessage: msg.Message, response: res}
			if err := <-res; err != nil {
				r.log.Errorf("Failed to route message: %v", err)
			}
		}
	}
}

func (r *router) handleSendRequests(
	sendRequests <-chan routedSendReq,
) {
	for req := range sendRequests {
		// Warn if the message is sent to self
		if r.self == req.To {
			r.log.Errorf("Attempted to send message to self: %v", req)
			req.response <- nil
			continue
		}

		// Check if we have a route to the destination, if so then send the message directly
		nextHop := r.table.LookupTable(req.To)
		if !nextHop.IsNone() {
			r.log.Infof("Routing message to %v through %v", req.To, nextHop)
			r.routerToNet <- messages.Destined[RoutedMessage]{Message: req.RoutedMessage, To: nextHop.Get()}
			req.response <- nil
			continue
		}

		// If we don't have a route, start a new pulse to explore the network
		r.log.Warnf("No route to %v", req.To)
		res, err := r.pulsar.StartPulse(ExplorationRequest{})
		if err != nil {
			r.log.Errorf("Failed to complete exploration: %v", err)
			req.response <- err
			continue
		}

		if res, ok := res.(ExplorationResponse); !ok {
			req.response <- fmt.Errorf("unexpected response type: %T", res)
			continue
		} else {
			// Update our lookup table with the merged results from all echoes
			r.table.UpdateTable(res.RoutingTable)
			// Lookup directly the next hop
			nextHop = r.table.LookupTable(req.To)
			if nextHop.IsNone() {
				req.response <- fmt.Errorf("no route to %v", req.To)
				continue
			}

			// Route the message to the next hop
			r.log.Infof("Routing message to %v through %v", req.To, nextHop)
			r.routerToNet <- messages.Destined[RoutedMessage]{Message: req.RoutedMessage, To: nextHop.Get()}
			req.response <- nil
		}
	}
}

// Helper method to join all received routing tables from all echoes
func (r *router) joinRoutingTables(
	echoes []pulsing.SourcedMessage[messages.Message],
) (table routingTable) {
	table = make(routingTable)
	for _, echo := range echoes {
		if resp, ok := echo.Msg.(ExplorationResponse); !ok {
			r.log.Warnf("Unknown echo type: %T", echo.Msg)
		} else {
			src := echo.From
			if src != r.self {
				table[src] = src
			}

			for dest := range resp.RoutingTable {
				// Route through the echo source
				if _, exists := table[dest]; !exists && dest != r.self && dest != src {
					table[dest] = src
				}
			}
		}
	}

	return
}

// getRoutingTable returns the current state of this process's routing table. Used for tests.
func (r *router) getRoutingTable() routingTable {
	return r.table.GetTable()
}

func (r *router) Broadcast(msg messages.Message) (receivers []transport.Address, err error) {
	echo, err := r.pulsar.StartPulse(BroadcastRequest{Msg: msg})
	if err != nil {
		return nil, err
	}

	if resp, ok := echo.(BroadcastResponse); !ok {
		return nil, fmt.Errorf("unexpected response type: %T", echo)
	} else {
		return resp.Receivers, nil
	}
}

func (r *router) ReceivedMessageChan() <-chan messages.Sourced[messages.Message] {
	return r.messageOutput
}

func (r *router) Send(msg messages.Message, dest transport.Address) error {
	res := make(chan error)
	r.sendRequests <- routedSendReq{RoutedMessage{Msg: msg, From: r.self, To: dest}, res}
	return <-res
}
