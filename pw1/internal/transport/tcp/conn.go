package tcp

import (
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"context"
	"net"
)

type connectionRegistration struct {
	conn net.Conn
	addr Address
}

type getConnection struct {
	addr       Address
	create     bool
	resChannel chan<- *connectionHandler
	errChannel chan<- error
}

func (tcp *TCP) listenIncomingConnections() error {
	defer close(tcp.listenCloseConfirm)

	listener, err := net.Listen("tcp", tcp.local.String())
	if err != nil {
		tcp.logger.Error("Error listening:", err)
		return err
	}

	defer listener.Close()
	go func() {
		<-tcp.closeChan
		_ = listener.Close()
	}()

	tcp.logger.Infof("Listening for incoming connections on %s", tcp.local)

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-tcp.closeChan:
				tcp.logger.Warn("TCP connection listener closed")
				return err
			default:
				tcp.logger.Error("Error accepting connection:", err)
				continue
			}
		}

		tcp.handleConnectionHandshake(conn)
	}
}

func (tcp *TCP) connectNeighbors(
	neighbors []Address,
	done chan<- struct{},
) {
	defer close(done)
	for _, neighbor := range neighbors {
		if neighbor.Port > tcp.local.Port {
			connChan := make(chan *connectionHandler)
			tcp.getConnections <- getConnection{neighbor, false, connChan, nil}
			<-connChan
			continue
		}

		err := utils.RetryWithCutoff(
			tcp.logger, context.Background(),
			func(try int) (err error) {
				tcp.logger.Infof("Attempting to connect to neighbor %v (attempt %d)", neighbor, try+1)

				connChan := make(chan *connectionHandler)
				errChan := make(chan error)
				tcp.getConnections <- getConnection{neighbor, true, connChan, errChan}
				select {
				case <-connChan:
				case err = <-errChan:
				}

				return
			},
		)

		if err != nil {
			tcp.logger.Errorf("Could not connect to neighbor %v", neighbor)
			continue
		}

		tcp.logger.Infof("Connected to neighbor %v", neighbor)
	}
}

func (tcp *TCP) handleConnections() {
	connections := make(map[Address]*connectionHandler)
	connectionSubs := make(map[Address][]chan<- *connectionHandler)

	defer func() {
		tcp.logger.Warn("TCP's state-handler is closing.")
		for _, conn := range connections {
			_ = conn.Close()
			delete(connections, conn.addr)
		}

		close(tcp.connCloseConfirm)
	}()

	for {
		select {
		case getter := <-tcp.getConnections:
			tcp.getConnection(getter, connections, connectionSubs)
		case conn := <-tcp.connectionRegistrations:
			tcp.handleConnection(conn.conn, conn.addr, connections, connectionSubs)
		case addr := <-tcp.connectionUnregistrations:
			tcp.logger.Infof("TCP unregistering connection from %s", addr)
			delete(connections, addr)
		case <-tcp.closeChan:
			return
		}
	}
}

func (tcp *TCP) getConnection(
	getter getConnection,
	activeConnections map[Address]*connectionHandler,
	connectionSubs map[Address][]chan<- *connectionHandler,
) {
	serverConn, ok := activeConnections[getter.addr]
	if ok && serverConn.IsClosed() {
		delete(activeConnections, getter.addr)
		ok = false
	}

	if !ok && !getter.create {
		wait := make(chan *connectionHandler)
		connectionSubs[getter.addr] = append(connectionSubs[getter.addr], wait)
		go func() {
			select {
			case conn := <-wait:
				getter.resChannel <- conn
			case <-tcp.closeChan:
			}
		}()
		return
	}

	if !ok {
		conn, err := tcp.connectRemote(getter.addr)
		if err != nil {
			getter.errChannel <- err
			return
		}

		tcp.handleConnection(conn, getter.addr, activeConnections, connectionSubs)
		serverConn = activeConnections[getter.addr]
	}

	getter.resChannel <- serverConn
}

func (tcp *TCP) handleConnection(
	conn net.Conn,
	addr Address,
	activeConnections map[Address]*connectionHandler,
	connectionSubs map[Address][]chan<- *connectionHandler,
) {
	if activeConn, ok := activeConnections[addr]; ok && !activeConn.IsClosed() {
		tcp.logger.Warnf("TCP connection from %s already exists. Closing the previous one.", addr)
		activeConn.Close()
	}

	delete(activeConnections, addr)
	tcp.logger.Infof("TCP registering connection from %s", addr)
	handler := tcp.newConnectionHandler(addr, conn)
	activeConnections[addr] = handler

	for _, sub := range connectionSubs[addr] {
		sub <- handler
	}
	delete(connectionSubs, addr)
}

func (tcp *TCP) connectRemote(addr transport.Address) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		return nil, err
	}

	err = tcp.sendHandshake(conn)
	if err != nil {
		tcp.logger.Error("Error sending handshake, aborting connection:", err)
		conn.Close()
		return nil, err
	}

	return conn, nil
}
