package tcp

import (
	"bytes"
	"encoding/gob"
)

type ReadAckMessage struct {
	Payload []byte
}

// Encodes the message to a byte slice
func (m *ReadAckMessage) encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(m)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decodes a message from a byte slice
func DecodeReadAckMessage(encodedMessage []byte) (*ReadAckMessage, error) {
	var message ReadAckMessage
	buf := bytes.NewBuffer(encodedMessage)
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&message)
	if err != nil {
		return nil, err
	}
	return &message, nil
}
