package tcp

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport/rr"
	"chatsapp/internal/utils"
	"fmt"
)

type remoteState uint8

const (
	connecting remoteState = iota
	connected
	disconnected
)

func (s remoteState) String() string {
	switch s {
	case connecting:
		return "connecting"
	case connected:
		return "connected"
	case disconnected:
		return "disconnected"
	default:
		return "unknown"
	}
}

// Represents a remote connection to another server. The sendChan channel is to be used to send messages to that remote.
//
// Note that this channel is not read until the connection is established.
//
// Note also that this structure is not defined by a connection to the remote. This is because it may be created before the connection is established, since we want to allow the user to be able to try sending messages to a remote immediately.
type remote struct {
	logger *logging.Logger

	rr rr.RR

	sendRequests *utils.BufferedChan[sendRequest]

	selfAddr   address
	remoteAddr address

	netToRR chan<- []byte
	rrToNet *utils.BufferedChan[[]byte]

	closeChan chan struct{}

	getState chan remoteState
	setState chan remoteState
}

func newRemote(logger *logging.Logger, selfAddr address, remoteAddr address, receivedMessages chan message) *remote {
	logger = logger.WithPostfix(fmt.Sprintf("(%v)", remoteAddr.Port))
	logger.Infof("Creating new remote for %s", remoteAddr)

	rrToNet := utils.NewBufferedChan[[]byte]()
	netToRR := utils.NewBufferedChan[[]byte]()

	wrapper := rr.NetWrapper{
		FromNet: netToRR.Outlet(),
		IntoNet: rrToNet.Inlet(),
	}

	logger.Info("New remote created for ", remoteAddr)
	r := remote{
		logger:       logger,
		selfAddr:     selfAddr,
		remoteAddr:   remoteAddr,
		rr:           rr.NewRR(logger.WithPostfix("rr"), remoteAddr, wrapper),
		netToRR:      netToRR.Inlet(),
		rrToNet:      rrToNet,
		closeChan:    make(chan struct{}),
		sendRequests: utils.NewBufferedChan[sendRequest](),
		getState:     make(chan remoteState),
		setState:     make(chan remoteState),
	}

	r.rr.SetRequestHandler(func(payload []byte) []byte {
		logger.Infof("Received message from %v. Forwarding it to the receivedMessages channel", remoteAddr)
		receivedMessages <- message{Source: remoteAddr, Payload: payload}
		return nil
	})

	go r.sequentializeSends()
	go r.handleState()

	return &r
}

func (r *remote) handleState() {
	state := disconnected
	for {
		select {
		case r.getState <- state:
		case newState := <-r.setState:
			state = newState
		}
	}
}

func (r *remote) SetState(state remoteState) {
	r.setState <- state
}

func (r *remote) GetState() remoteState {
	return <-r.getState
}

// Main goroutine for handling a given remote connection. It listens for
//   - incoming messages and sends them to the [receivedMessages] channel;
//   - messages sent on [sentChan] and sends them to the remote.
func (r *remote) handleConn(codec connCodec, andThen func()) {
	r.setState <- connected

	r.logger.Info("Handling remote ", r.remoteAddr)

	stopSending := make(chan struct{})

	defer func() {
		r.logger.Warn("handleConn closed due to closed conn or other error")
		close(stopSending)

		andThen()
	}()

	go func() {
		for {
			select {
			case <-r.closeChan:
				r.logger.Warn("handleConn's sender is closing due to close request")
				codec.Close()
				return
			case payload := <-r.rrToNet.Outlet():
				r.logger.Info("RR requests to send message to ", r.remoteAddr)
				msg := message{Source: r.selfAddr, Payload: payload}
				err := codec.SendMessage(msg)
				if err != nil {
					r.logger.Error("Error encoding message in transport, ignoring it. ", err)
				}
			case <-stopSending:
				r.logger.Warn("handleConn's sender is closing due to closed conn")
				return
			}
		}
	}()

	for {
		msg, err := codec.Receive()
		if err != nil {
			r.logger.Error("Error decoding message in transport: ", err)
			break
		}

		if msg.Source != r.remoteAddr {
			r.logger.Error("Received message from unexpected source. Got ", msg.Source, ", expected ", r.remoteAddr, ".")
			continue
		}

		r.logger.Info("Received message from ", msg.Source, ". Forwarding it to RR")

		r.netToRR <- msg.Payload
	}
}

func (r *remote) sequentializeSends() {
	for req := range r.sendRequests.Outlet() {
		_, err := r.rr.SendRequest(req.payload)
		req.err <- err
	}
}

func (r *remote) Close() {
	close(r.sendRequests.Inlet())
	r.rr.Close()
	close(r.closeChan)
}
