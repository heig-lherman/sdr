package pulsing

import (
	"chatsapp/internal/transport"
	"chatsapp/internal/utils"
	"encoding/gob"
	"fmt"
)

// PulseID is a unique identifier for a pulse
type PulseID struct {
	Source transport.Address
	utils.UID
}

func (id PulseID) String() string {
	return fmt.Sprintf("%v-%v", id.Source, id.UID)
}

// NewPulseID creates a new pulse ID with the given source address and UID.
func NewPulseID(source transport.Address, id utils.UID) PulseID {
	return PulseID{Source: source, UID: id}
}

// Type is the type of a pulsar message.
type Type int

const (
	// Pulse is the type given to a pulse message.
	Pulse Type = iota
	// Echo is the type given to an echo message.
	Echo
)

// SourcedMessage is a message with a source address.
type SourcedMessage[M any] struct {
	Msg  M
	From transport.Address
}

// PulsarMessage is the type of messages a pulsar uses to communicate with other pulsars.
type PulsarMessage[M any] struct {
	Type    Type
	Payload M
	ID      PulseID
}

func NewPulseMessage[M any](payload M, id PulseID) PulsarMessage[M] {
	return PulsarMessage[M]{Type: Pulse, Payload: payload, ID: id}
}

func NewEchoMessage[M any](payload M, id PulseID) PulsarMessage[M] {
	return PulsarMessage[M]{Type: Echo, Payload: payload, ID: id}
}

func (p PulsarMessage[M]) RegisterToGob() {
	gob.Register(p)
	gob.Register(PulseID{})
}

func (p PulsarMessage[M]) String() string {
	tp := "Unknown"
	switch p.Type {
	case Pulse:
		tp = "Pulse"
	case Echo:
		tp = "Echo"
	}
	return fmt.Sprintf("Pulsar-%v{%v, %v}", tp, p.ID, p.Payload)
}
