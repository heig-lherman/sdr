package server

import (
	"bytes"
	"chatsapp/internal/common"
	"chatsapp/internal/transport"
	"encoding/gob"
)

type Address = transport.Address

// Generic message type
type Message interface {
	RegisterToGob()
}

// A chat message sent by a given user. Can be sent from server to client or from client to server.
//
// If sent from the client, the server will ignore it if the user field does not match the user that the client is connected as.
type Username = common.Username

type ChatMessage struct {
	User    Username
	Content string
}

func (ChatMessage) RegisterToGob() {
	gob.Register(ChatMessage{})
}

// Encode a message to a byte slice
func EncodeMessage(m Message) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(&m)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode a message from a byte slice
func DecodeMessage(data []byte) (Message, error) {
	var m Message
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&m)
	if err != nil {
		return nil, err
	}
	return m, nil
}
