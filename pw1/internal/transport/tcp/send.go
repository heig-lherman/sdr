package tcp

type destinedMessage struct {
	addr       Address
	payload    []byte
	errChannel chan<- error
}

func (tcp *TCP) handleSendRequests() {
	for {
		select {
		case msg := <-tcp.sendRequests:
			connChan := make(chan *connectionHandler)
			errChan := make(chan error)
			tcp.getConnections <- getConnection{msg.addr, true, connChan, errChan}

			select {
			case conn := <-connChan:
				msg.errChannel <- conn.Send(msg)
			case err := <-errChan:
				msg.errChannel <- err
			case <-tcp.closeChan:
				return
			}
		case <-tcp.closeChan:
			return
		}
	}
}
