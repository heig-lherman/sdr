package tcp

import (
	"encoding/gob"
	"net"
)

func (tcp *TCP) sendHandshake(conn net.Conn) error {
	tcp.logger.Infof("%v->%v - sending handshake", conn.LocalAddr(), conn.RemoteAddr())

	encoder := gob.NewEncoder(conn)
	err := encoder.Encode(&Message{
		Source:  tcp.local,
		Payload: nil,
	})

	return err
}

func (tcp *TCP) handleConnectionHandshake(conn net.Conn) {
	tcp.logger.Infof("%v<-%v - awaiting handshake", conn.LocalAddr(), conn.RemoteAddr())

	decoder := gob.NewDecoder(conn)
	var handshake Message
	err := decoder.Decode(&handshake)
	if err != nil {
		tcp.logger.Error("Could not receive handshake message from remote:", err)
		conn.Close()
		return
	}

	tcp.logger.Infof("%v<-%v (%v) - received handshake", conn.LocalAddr(), conn.RemoteAddr(), handshake.Source)
	tcp.connectionRegistrations <- connectionRegistration{conn, handshake.Source}
}
