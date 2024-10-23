package tcp

import (
	"math/rand"
	"net"
	"time"
)

type destinedMessage struct {
	addr       Address
	payload    []byte
	errChannel chan<- error
}

type handlerRegistration struct {
	id      HandlerId
	handler MessageHandler
}

type connectionRegistration struct {
	conn net.Conn
	addr Address
}

func (tcp *TCP) handleState(
	initialNeighbors []Address,
) {
	registeredHandlers := make(map[HandlerId]MessageHandler)
	connections := make(map[Address]*connectionHandler)

	time.Sleep(time.Duration(rand.Intn(200)+300) * time.Millisecond)
	for _, neighbor := range initialNeighbors {
		if neighbor.Port > tcp.local.Port {
			continue
		}

		conn, err := tcp.connectRemote(neighbor)
		if err != nil {
			tcp.logger.Errorf("Error connecting to neighbor %v: %v", neighbor, err)
			continue
		}

		tcp.logger.Infof("Connected to neighbor %v", neighbor)
		tcp.handleConnection(conn, neighbor, connections)
	}

	defer func() {
		tcp.logger.Warn("TCP's state-handler is closing.")
		for _, conn := range connections {
			_ = conn.Close()
			delete(connections, conn.addr)
		}

		close(tcp.closeConfirm)
	}()

	for {
		select {
		case msg := <-tcp.sendRequests:
			tcp.handleSend(msg, connections)
		case msg := <-tcp.receivedMessages:
			tcp.handleReceive(msg, registeredHandlers)
		case msg := <-tcp.readAcknowledgements:
			if len(registeredHandlers) > 1 {
				tcp.handleReceive(msg, registeredHandlers)
			}
		case registration := <-tcp.handlerRegistrations:
			tcp.logger.Infof("TCP registering handler %d", registration.id)
			registeredHandlers[registration.id] = registration.handler
		case id := <-tcp.handlerUnregistrations:
			tcp.logger.Infof("TCP unregistering handler %d", id)
			delete(registeredHandlers, id)
		case conn := <-tcp.connectionRegistrations:
			tcp.handleConnection(conn.conn, conn.addr, connections)
		case addr := <-tcp.connectionUnregistrations:
			tcp.logger.Infof("TCP unregistering connection from %s", addr)
			delete(connections, addr)
		case <-tcp.closeChan:
			return
		}
	}
}

func (tcp *TCP) handleConnection(
	conn net.Conn,
	addr Address,
	activeConnections map[Address]*connectionHandler,
) {
	if activeConn, ok := activeConnections[addr]; ok && !activeConn.IsClosed() {
		tcp.logger.Warnf("TCP connection from %s already exists. Closing the previous one.", addr)
		activeConn.Close()
	}

	delete(activeConnections, addr)
	tcp.logger.Infof("TCP registering connection from %s", addr)
	activeConnections[addr] = tcp.newConnectionHandler(addr, conn)
}

func (tcp *TCP) handleSend(
	msg destinedMessage,
	activeConnections map[Address]*connectionHandler,
) {
	serverConn, ok := activeConnections[msg.addr]
	if !ok || serverConn.IsClosed() {
		conn, err := tcp.connectRemote(msg.addr)
		if err != nil {
			msg.errChannel <- err
			return
		}

		tcp.handleConnection(conn, msg.addr, activeConnections)
		serverConn = activeConnections[msg.addr]
	}

	go func() {
		msg.errChannel <- serverConn.Send(msg)
	}()
}

func (tcp *TCP) handleReceive(
	msg Message,
	registeredHandlers map[HandlerId]MessageHandler,
) {
	tcp.logger.Info("TCP received message from", msg.Source, ". Dispatching it among ", len(registeredHandlers), " handlers.")
	for _, handler := range registeredHandlers {
		if handler.HandleNetworkMessage(&msg) {
			return
		}
	}
}
