package common

import (
	"bytes"
	"chatsapp/internal/transport"
	"encoding/gob"
	"fmt"
)

type Address = transport.Address
type Username string

// Generic message type
type Message interface{}

// Request by client to connect to the destination server
//   - User: The username on behalf of whom the client is connecting
type ConnRequestMessage struct {
	User Username
}

// Response by the server for a connection request, indicating the address of the server that the client should connect to, called the leader. If the leader is the same as the server that sent this response, it indicates to the client that this server accepts the connection, and it may start sending and receiving messages.
//   - Leader: The address of the leader server
type ConnResponseMessage struct {
	Leader Address
}

func (ConnRequestMessage) RegisterToGob() {
	gob.Register(ConnRequestMessage{})
}

func (ConnResponseMessage) RegisterToGob() {
	gob.Register(ConnResponseMessage{})
}

// A chat message sent by a given user. Can be sent from server to client or from client to server.
//
// If sent from the client, the server will ignore it if the user field does not match the user that the client is connected as.
type ChatMessage struct {
	User    Username
	Content string
}

func (ChatMessage) RegisterToGob() {
	gob.Register(ChatMessage{})
}

// A message indicating that the client is closing the connection
type ConnClose struct {
	User Username
}

func (ConnClose) RegisterToGob() {
	gob.Register(ConnClose{})
}

// Register all message types to gob. Required for encoding and decoding messages
func RegisterAllToGob() {
	gob.Register(ConnRequestMessage{})
	gob.Register(ConnResponseMessage{})
	gob.Register(ChatMessage{})
	gob.Register(ConnClose{})
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

func (m ConnRequestMessage) String() string {
	return fmt.Sprintf("ConnReq{%s}", m.User)
}

func (m ConnResponseMessage) String() string {
	return fmt.Sprintf("ConnResp{%s}", m.Leader)
}

func (m ChatMessage) String() string {
	return fmt.Sprintf("Chat{%s: %s}", m.User, m.Content)
}

func (m ConnClose) String() string {
	return fmt.Sprintf("ConnClose{%s}", m.User)
}
