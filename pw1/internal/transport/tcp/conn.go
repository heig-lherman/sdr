package tcp

import (
	"chatsapp/internal/transport"
	"net"
)

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

func (tcp *TCP) listenIncomingConnections() error {
	listener, err := net.Listen("tcp", tcp.local.String())
	if err != nil {
		tcp.logger.Error("Error listening:", err)
		return err
	}

	tcp.logger.Infof("Listening for incoming connections on %s", tcp.local)

	defer listener.Close()
	go func() {
		<-tcp.closeChan
		_ = listener.Close()
	}()

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

		go tcp.handleConnectionHandshake(conn)
	}
}
