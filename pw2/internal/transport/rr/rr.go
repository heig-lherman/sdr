package rr

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"encoding/gob"
	"fmt"
	"time"
)

// Sequence number
type seqnum struct {
	// ID of the RR instance that generated that sequence number. Could be UNIX time in milliseconds, for example.
	InstId int64
	// ID of the message associated with that sequence number. Must be unique in the context of the RR instance identified by InstId.
	MsgId int64
}

// Compares two sequence numbers ; by instance id and then by message id.
func (s seqnum) LessThan(other seqnum) bool {
	return s.InstId < other.InstId || (s.InstId == other.InstId && s.MsgId < other.MsgId)
}

// Implements the Stringer interface for sequence numbers.
func (s seqnum) String() string {
	return fmt.Sprintf("%d-%d", s.InstId, s.MsgId)
}

// Represents an instance of a Request-Response (RR) protocol.
//
// This abstraction allows this server to be used as both sender and receiver in the RR protocol, for a given remote. It is thus intended that there be one RR instance per known remote.
type RR interface {
	// Sends a request to the associated remote, and blocks until a response is received from that remote.
	SendRequest(payload []byte) (response []byte, err error)
	// Sets the handler for incoming requests. The handler should return the response that should be replied to the sender. Response may be nil in which case empty response will be sent to the sender.
	SetRequestHandler(handleRequest func([]byte) []byte)
	// Destroys the RR instance, cleaning up resources.
	Close()
	// Sets the duration to wait before sending a response to a request. Useful for testing.
	SetSlowdown(duration time.Duration)
}

// Contains means of communication with the network for the RR protocol. RR instances will use this struct's channels to send and receive messages to/from the corresponding remote. This allows the RR protocol to be oblivious to the underlying network implementation. Helpful for testing, in particular.
type RRNetWrapper struct {
	// The channel on which the RR instance will send messages to the remote.
	IntoNet chan<- []byte
	// The channel on which the RR instance will receive messages from the remote.
	FromNet <-chan []byte
}

// Represents an internal request to send a Request-type message.
type sendRequest struct {
	payload  []byte
	response chan []byte
}

// Implementation of the RR interface. Hiding the implementation behind an interface allows for easier testing.
type rrImpl struct {
	creationTime int64 // nano seconds since epoch
	nextSeqnum   utils.UIDGenerator

	logger   *logging.Logger
	slowdown time.Duration

	destinationAddr transport.Address
	network         RRNetWrapper

	onRequest func([]byte) []byte

	sendRequests      chan sendRequest
	receivedRequests  chan *message
	receivedResponses chan *message

	closeChan chan struct{}
}

// Creates a new RR instance.
//   - log: The logger to use for logging messages.
//   - destinationAddr: The address of the remote to associate with this RR instance.
//   - networkInterface: The network interface to use for sending and receiving messages.
func NewRR(log *logging.Logger, destinationAddr transport.Address, network RRNetWrapper) RR {
	log.Infof("Creating new RR for %v", destinationAddr)

	rr := rrImpl{
		creationTime:    time.Now().UnixNano(),
		logger:          log,
		destinationAddr: destinationAddr,
		nextSeqnum:      utils.NewUIDGenerator(),
		network:         network,
		closeChan:       make(chan struct{}),
	}

	gob.Register(message{})

	rr.sendRequests = make(chan sendRequest)
	rr.receivedRequests = make(chan *message)
	rr.receivedResponses = make(chan *message)

	go rr.handleSendRequests()
	go rr.handleReceiveRequests()
	go rr.dispatchFromNetwork()

	return &rr
}

func (rr *rrImpl) dispatchFromNetwork() {
	for {
		select {
		case <-rr.closeChan:
			rr.logger.Warn("Killing RR's fromNet receptor")
			return
		case msg := <-rr.network.FromNet:

			message, err := decode(msg)
			if err != nil {
				rr.logger.Errorf("Error decoding message: %s. Ignoring", err)
				continue
			}

			rr.logger.Info("RR received message of type ", message.Type, ", handling it as such...")

			switch message.Type {
			case reqMsg:
				rr.receivedRequests <- message
			case rspMsg:
				rr.receivedResponses <- message
			}
		}
	}
}

func (rr *rrImpl) sendToNet(bytes []byte) {
	rr.network.IntoNet <- bytes
}

func (rr *rrImpl) SetSlowdown(duration time.Duration) {
	rr.slowdown = duration
}

func (rr *rrImpl) Close() {
	rr.logger.Info("Closing RR")
	close(rr.closeChan)
}

// Main goroutine for sending requests. It sends requests to the remote address and waits for a response.
func (rr *rrImpl) handleSendRequests() {
	<-rr.nextSeqnum // Skip 0

	var n seqnum

	rr.logger.Info("Starting handleSendRequests")

	for {
		select {
		case <-rr.closeChan:
			rr.logger.Warn("Killing handleSendRequests")
			return
		case sendRequest := <-rr.sendRequests:
			rr.logger.Info("Received send request")

			nextMsgId := <-rr.nextSeqnum
			n = seqnum{rr.creationTime, int64(nextMsgId)}

			message := message{
				Type:    reqMsg,
				Seqnum:  n,
				Payload: sendRequest.payload,
			}

			bytes, err := message.encode()
			if err != nil {
				rr.logger.Error("Error encoding message:", err)
				continue
			}

			rr.logger.Info("Sending request with seqnum", n)
			rr.sendToNet(bytes)

			rr.logger.Info("waiting for response for seqnum", n)
			tryAgain := true
			for tryAgain {
				select {
				case <-rr.closeChan:
					rr.logger.Warn("Killing one of handleSendRequests chan's response handler")
					return
				case response := <-rr.receivedResponses:
					rr.logger.Info("received response for seqnum", response.Seqnum)
					if response.Seqnum == n {
						sendRequest.response <- response.Payload
						close(sendRequest.response)
						tryAgain = false
					} else {
						rr.logger.Warn("Warning: Received response with seqnum", response.Seqnum, "different from last seqnum", n)
					}
				case <-time.After(500 * time.Millisecond):
					rr.logger.Warn("timeout for seqnum", n)
					rr.sendToNet(bytes)
				}
			}
		}
	}
}

// Main goroutine for receiving requests. It processes incoming requests and sends responses.
func (r *rrImpl) handleReceiveRequests() {
	lastSeqnum := seqnum{0, -1}
	lastResponse := []byte(nil)

	for {
		select {
		case <-r.closeChan:
			r.logger.Warn("Killing handleReceiveRequests")
			return
		case msg := <-r.receivedRequests:
			r.logger.Info("Received request with seqnum", msg.Seqnum)
			seqnum := msg.Seqnum

			if r.slowdown > 0 {
				time.Sleep(r.slowdown)
				select {
				case <-r.closeChan:
					r.logger.Warn("Killing handleReceiveRequests")
					return
				default:
				}
			}

			if seqnum == lastSeqnum {
				if lastResponse != nil {
					r.logger.Info("Request already processed. Resending Response.")
					// Resend response
					r.sendToNet(lastResponse)
				} else {
					r.logger.Warn("Request already received, but no known response. Ignoring")
				}
			} else if lastSeqnum.LessThan(seqnum) {
				r.logger.Info("Request is new. Processing...")
				// Handle request
				lastResponse = nil
				lastSeqnum = seqnum

				if r.onRequest == nil {
					r.logger.Error("Warning: No onRequest handler set. Ignoring request.")
					continue
				}
				response := r.onRequest(msg.Payload)
				if response == nil {
					r.logger.Info("onRequest handler returned nil response. Responding with empty payload.")
				}

				rrResponse := message{
					Type:    rspMsg,
					Seqnum:  msg.Seqnum,
					Payload: response,
				}

				bytes, err := rrResponse.encode()
				if err != nil {
					r.logger.Error("Error encoding response:", err)
					return
				}

				lastResponse = bytes

				r.sendToNet(bytes)
			} else {
				r.logger.Warn("Warning: Received request with seqnum", seqnum, "less than last seqnum", lastSeqnum)
			}
		}
	}
}

func (r *rrImpl) SendRequest(payload []byte) (response []byte, err error) {
	responseChan := make(chan []byte, 1)

	r.sendRequests <- sendRequest{
		payload:  payload,
		response: responseChan,
	}

	return <-responseChan, nil
}

func (r *rrImpl) SetRequestHandler(handleRequest func([]byte) []byte) {
	if r.onRequest != nil {
		r.logger.Warn("Warning: Overwriting existing onRequest handler")
	}
	r.onRequest = handleRequest
}
