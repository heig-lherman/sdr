package tcp

import (
	"encoding/gob"
	"net"
)

// A TCP connection wrapper that stores an encoder and decoder for the connection.
type connCodec struct {
	conn    net.Conn
	encoder *gob.Encoder
	decoder *gob.Decoder
}

// A TCP connection wrapper that includes the remote address
type addressedCodec struct {
	remote Address
	connCodec
}

// Constructs and returns a new [tcpConnection] instance from a given [net.Conn] instance.
func newConnCodec(conn net.Conn) connCodec {
	return connCodec{
		conn:    conn,
		encoder: gob.NewEncoder(conn),
		decoder: gob.NewDecoder(conn),
	}
}

// Constructs and returns a new [tcpAddressedConnection] instance from a given [tcpConnection] instance and a remote address.
func (c connCodec) WithAddress(addr Address) addressedCodec {
	return addressedCodec{
		remote:    addr,
		connCodec: c,
	}
}

func (c connCodec) Close() {
	c.conn.Close()
}

// Returns the next message received from the connection. Blocks until a message is received.
func (c connCodec) Receive() (Message, error) {
	var msg Message
	err := c.decoder.Decode(&msg)
	if err != nil {
		return Message{}, err
	}
	return msg, nil
}

// Sends a message through the connection.
func (c connCodec) SendMessage(msg Message) error {
	return c.encoder.Encode(msg)
}
