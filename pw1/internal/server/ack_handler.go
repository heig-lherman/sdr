package server

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/transport/tcp"
	"chatsapp/internal/utils/ioUtils"
	"fmt"
)

type ReadAckHandler struct {
	ioStream ioUtils.IOStream
	logger   *logging.Logger
}

func NewReadAckHandler(ioStream ioUtils.IOStream, logger *logging.Logger) *ReadAckHandler {
	return &ReadAckHandler{ioStream: ioStream, logger: logger}
}

func (h *ReadAckHandler) HandleNetworkMessage(message *transport.Message) (wasHandled bool) {
	wasHandled = true

	ackMsg, err := tcp.DecodeReadAckMessage(message.Payload)
	if err != nil {
		return false
	}

	chatMsg, err := decodeMessage(ackMsg.Payload)
	if err != nil {
		return false
	}

	h.logger.Infof("[READ ACK] from %s: %s", message.Source, chatMsg.Content)
	h.ioStream.Println(fmt.Sprintf("[%s received: %s]", message.Source, chatMsg.Content))
	return
}
