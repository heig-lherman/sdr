package server

import (
	"bytes"
	"chatsapp/internal/common"
	"chatsapp/internal/messages"
	"encoding/gob"
)

// ChatMessage is a chat message sent by a given user. Can be sent from server to client or from client to server.
//
// If sent from the client, the server will ignore it if the user field does not match the user that the client is connected as.
type ChatMessage struct {
	User    common.Username
	Content string
}

// RegisterToGob registers this message type to Gob.
func (ChatMessage) RegisterToGob() {
	gob.Register(ChatMessage{})
}

// EncodeMessage encodes a message to a byte slice
func EncodeMessage(m messages.Message) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(&m)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeMessage decodes a message from a byte slice
func DecodeMessage(data []byte) (messages.Message, error) {
	var m messages.Message
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&m)
	if err != nil {
		return nil, err
	}
	return m, nil
}
