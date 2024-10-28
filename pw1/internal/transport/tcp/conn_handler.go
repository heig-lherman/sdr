package tcp

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport/rr"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
)

// connectedServer represents a server connected to the local node, with the ability to send payloads to it.
type connectionHandler struct {
	tcp *TCP
	log *logging.Logger

	addr Address
	conn net.Conn
	rr   rr.RR

	closeChan            chan struct{}
	confirmCloseIncoming chan struct{}
	confirmCloseOutgoing chan struct{}
}

func (tcp *TCP) newConnectionHandler(addr Address, conn net.Conn) *connectionHandler {
	handler := connectionHandler{
		tcp: tcp,
		log: tcp.logger.WithPostfix(fmt.Sprintf("handler(%v)", addr)),

		addr: addr,
		conn: conn,

		closeChan:            make(chan struct{}),
		confirmCloseIncoming: make(chan struct{}),
		confirmCloseOutgoing: make(chan struct{}),
	}

	incoming := make(chan []byte)
	outgoing := make(chan []byte)

	go handler.listenIncomingMessages(incoming)
	go handler.listenOutgoingMessages(outgoing)

	rr := rr.NewRR(
		tcp.logger.WithPostfix("rr"),
		addr,
		rr.RRNetWrapper{
			Incoming: incoming,
			Outgoing: outgoing,
		},
	)
	rr.SetRequestHandler(func(payload []byte) []byte {
		tcp.receivedMessages <- Message{Source: addr, Payload: payload}
		return payload
	})

	handler.rr = rr
	return &handler
}

func (ch *connectionHandler) Send(msg destinedMessage) error {
	_, err := ch.rr.SendRequest(msg.payload)
	if err != nil {
		return err
	}

	return nil
}

func (ch *connectionHandler) IsClosed() bool {
	select {
	case <-ch.closeChan:
		return true
	default:
		return false
	}
}

func (ch *connectionHandler) Close() (err error) {
	if ch.IsClosed() {
		return
	}

	close(ch.closeChan)

	ch.rr.Close()
	if ch.conn != nil {
		err = ch.conn.Close()
	}

	if !ch.tcp.IsClosed() {
		// This connection handler is closed due to a remote issue.
		ch.tcp.connectionUnregistrations <- ch.addr
	}

	<-ch.confirmCloseIncoming
	<-ch.confirmCloseOutgoing
	return
}

func (ch *connectionHandler) restartConnection() (err error) {
	ch.conn.Close()
	ch.conn, err = ch.tcp.connectRemote(ch.addr)
	if err != nil {
		ch.log.Errorf("Error restarting connection: %v", err)
		ch.Close()
	}

	return
}

func (ch *connectionHandler) listenIncomingMessages(incoming chan<- []byte) {
	defer close(ch.confirmCloseIncoming)
	for {
		var receivedMessage Message
		dec := gob.NewDecoder(ch.conn)
		err := dec.Decode(&receivedMessage)
		if err != nil {
			select {
			case <-ch.closeChan:
				ch.log.Warn("TCP's receive-handler closed due to connection close request")
				return
			default:
				if errors.Is(err, io.EOF) {
					ch.log.Warnf("Connection closed by remote at %v, restarting conn", ch.addr)
					err = ch.restartConnection()
					if err != nil {
						return
					}
				}

				ch.log.Error("Error decoding message in transport:", err)
				continue
			}
		}

		incoming <- receivedMessage.Payload
	}
}

func (ch *connectionHandler) listenOutgoingMessages(outgoing <-chan []byte) {
	defer close(ch.confirmCloseOutgoing)
	for {
		enc := gob.NewEncoder(ch.conn)
		select {
		case payload := <-outgoing:
			ch.log.Infof("TCP sending message to %v", ch.addr)
			message := Message{Source: ch.tcp.local, Payload: payload}
			err := enc.Encode(message)
			if err != nil {
				var operr *net.OpError
				if errors.As(err, &operr) {
					ch.log.Warnf("Transmission error, restarting conn: %v", operr)
					err = ch.restartConnection()
					if err != nil {
						return
					}
				}

				ch.log.Errorf("Error encoding message in transport: %v", err)
			}
		case <-ch.closeChan:
			ch.log.Warn("TCP's send-handler closed due to TCP handler closing")
			return
		}
	}
}
