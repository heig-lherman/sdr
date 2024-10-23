package rr

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"fmt"
	"time"
)

const (
	// Default timeout for waiting for a response to a request.
	defaultTimeout = 1500 * time.Millisecond
)

type requestHandler = func([]byte) []byte

// Sequence number
type seqnum struct {
	// ID of the RR instance that generated that sequence number. Could be UNIX time in milliseconds, for example.
	InstId uint64
	// ID of the message associated with that sequence number. Must be unique in the context of the RR instance identified by InstId.
	MsgId uint32
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
	SetRequestHandler(handleRequest requestHandler)
	// Destroys the RR instance, cleaning up resources.
	Close()
	// Sets the duration to wait before sending a response to a request. Useful for testing.
	SetSlowdown(duration time.Duration)
}

// Contains means of communication with the network for the RR protocol. RR instances will use this struct's channels to send and receive messages to/from the corresponding remote. This allows the RR protocol to be oblivious to the underlying network implementation. Helpful for testing, in particular.
type RRNetWrapper struct {
	// The channel on which the RR instance will send messages to the remote.
	Outgoing chan<- []byte
	// The channel on which the RR instance will receive messages from the remote.
	Incoming <-chan []byte
}

// sendRequest represents a request to send a payload to the remote and a channel on which to send the response.
type sendRequest struct {
	payload  []byte
	response chan<- []byte
	error    chan<- error
}

// Implementation of the RR interface. Hiding the implementation behind an interface allows for easier testing.
type rrImpl struct {
	// Context
	logger         *logging.Logger
	msgIdGenerator utils.UIDGenerator
	instanceId     uint64
	network        RRNetWrapper

	// Channels
	sendRequests     chan sendRequest
	incomingRequests chan message
	incomingReplies  chan message

	newSlowdown       chan time.Duration
	newRequestHandler chan requestHandler

	closeChan chan struct{}
}

// Creates a new RR instance.
//   - logger: The logger to use for logging messages.
//   - remoteAddr: The address of the remote to associate with this RR instance.
//   - network: The RRNetWrapper instance to use for communication with the remote.
func NewRR(logger *logging.Logger, remoteAddr transport.Address, network RRNetWrapper) RR {
	rr := rrImpl{
		logger:            logger.WithPostfix(fmt.Sprintf("to(%v)", remoteAddr)),
		msgIdGenerator:    utils.NewUIDGenerator(),
		instanceId:        uint64(time.Now().UnixMilli()),
		network:           network,
		sendRequests:      make(chan sendRequest),
		incomingRequests:  make(chan message),
		incomingReplies:   make(chan message),
		newSlowdown:       make(chan time.Duration),
		newRequestHandler: make(chan requestHandler),
		closeChan:         make(chan struct{}),
	}

	go rr.processSends()
	go rr.processReplies()
	go rr.listenIncoming()

	return &rr
}

func (rr *rrImpl) SendRequest(payload []byte) (response []byte, err error) {
	responseChannel := make(chan []byte)
	errorChannel := make(chan error)

	select {
	case rr.sendRequests <- sendRequest{payload, responseChannel, errorChannel}:
	case <-rr.closeChan:
		return nil, fmt.Errorf("RR instance closed while sending request")
	}

	select {
	case response = <-responseChannel:
		return response, nil
	case err = <-errorChannel:
		return nil, err
	case <-rr.closeChan:
		return nil, fmt.Errorf("RR instance closed while waiting for response")
	}
}

// processSends is the main goroutine that handles sending requests and waits for its response.
// It will block receiving requests to send while waiting for a response to a previous request.
func (rr *rrImpl) processSends() {
	for {
		var activeRequest sendRequest
		select {
		case activeRequest = <-rr.sendRequests:
		case <-rr.closeChan:
			rr.logger.Warn("RR send processing instance is closing.")
			return
		}

		rr.handleSendRequest(activeRequest)
	}
}

func (rr *rrImpl) handleSendRequest(activeRequest sendRequest) {
	rr.logger.Info("Received request to send, starting send")
	sendSeq := rr.generateSeqNum()
	requestMessage := newRequestMessage(sendSeq, activeRequest.payload)
	if err := rr.sendMessage(requestMessage); err != nil {
		activeRequest.error <- err
		return
	}

	for {
		select {
		case <-time.After(defaultTimeout):
			rr.logger.Warn("Request timed out. Resending...")
			if err := rr.sendMessage(requestMessage); err != nil {
				activeRequest.error <- err
				return
			}
		case msg := <-rr.incomingReplies:
			if msg.Seqnum.LessThan(sendSeq) {
				rr.logger.Info("Ignoring already received response:", msg.Seqnum)
				continue
			}

			activeRequest.response <- msg.Payload
			return
		case <-rr.closeChan:
			rr.logger.Warn("RR instance closed while waiting for a response.")
			return
		}
	}
}

// processReplies is the main goroutine that handles incoming requests and sends responses.
func (rr *rrImpl) processReplies() {
	var lastSentSeq seqnum
	var lastSentPayload []byte

	var reqHandler requestHandler
	var slowdown time.Duration

	for {
		select {
		case msg := <-rr.incomingRequests:
			if slowdown > 0 {
				time.Sleep(slowdown)
			}

			if msg.Seqnum.LessThan(lastSentSeq) {
				rr.logger.Info("Ignoring stale request:", msg.Seqnum)
				continue
			}

			if msg.Seqnum == lastSentSeq {
				rr.logger.Info("Received duplicate request:", msg.Seqnum)
			} else {
				lastSentSeq = msg.Seqnum
				lastSentPayload = make([]byte, 0)
				if reqHandler != nil {
					lastSentPayload = reqHandler(msg.Payload)
				}
			}

			responseMsg := newResponseMessage(lastSentSeq, lastSentPayload)
			if err := rr.sendMessage(responseMsg); err != nil {
				rr.logger.Error("Error sending response: ", err)
			}
		case newSlowdown := <-rr.newSlowdown:
			slowdown = newSlowdown
		case newReqHandler := <-rr.newRequestHandler:
			reqHandler = newReqHandler
		case <-rr.closeChan:
			rr.logger.Warn("RR reply processing instance is closing.")
			return
		}
	}
}

// listenIncoming is the main goroutine that listens for incoming messages from the network.
func (rr *rrImpl) listenIncoming() {
	rr.logger.Info("Listening for incoming requests...")
	for {
		select {
		case msgBytes := <-rr.network.Incoming:
			msg, err := decode(msgBytes)
			if err != nil {
				rr.logger.Error("Error decoding RR message: ", err)
				continue
			}

			switch msg.Type {
			case reqMsg:
				rr.incomingRequests <- *msg
			case rspMsg:
				rr.incomingReplies <- *msg
			default:
				rr.logger.Warn("Unknown message type: ", msg.Type)
			}
		case <-rr.closeChan:
			rr.logger.Warn("RR network listener is closing.")
			return
		}
	}
}

// generateSeqNum generates a new sequence number for the RR instance.
func (rr *rrImpl) generateSeqNum() seqnum {
	return seqnum{
		InstId: rr.instanceId,
		MsgId:  <-rr.msgIdGenerator,
	}
}

// sendMessage serializes and sends a message to the remote.
func (rr *rrImpl) sendMessage(msg *message) error {
	msgBytes, err := msg.encode()
	if err != nil {
		rr.logger.Error("Error encoding message: ", err)
		return err
	}

	return rr.sendOutgoing(msgBytes)
}

// sendOutgoing sends a message to the remote.
func (rr *rrImpl) sendOutgoing(msg []byte) error {
	select {
	case rr.network.Outgoing <- msg:
		return nil
	case <-rr.closeChan:
		return fmt.Errorf("RR instance closed")
	}
}

func (rr *rrImpl) Close() {
	close(rr.closeChan)
}

func (rr *rrImpl) SetRequestHandler(handleRequest func([]byte) []byte) {
	rr.newRequestHandler <- handleRequest
	rr.logger.Info("Request handler set")
}

func (rr *rrImpl) SetSlowdown(duration time.Duration) {
	rr.newSlowdown <- duration
	rr.logger.Infof("Slowdown set to %v", duration)
}
