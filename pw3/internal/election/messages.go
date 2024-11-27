package election

import (
	"encoding/gob"
	"fmt"
)

// Genering type for elector-related messages
type message interface{}

type announcement struct {
	Ability Ability
	Addr    address
}

type announcementMessage struct {
	Participants []announcement
}

func startAnnouncement(ability Ability, addr address) announcementMessage {
	return announcementMessage{[]announcement{{ability, addr}}}
}

func (announcementMessage) RegisterToGob() {
	gob.Register(address{})
	gob.Register(announcement{})
	gob.Register(announcementMessage{})
}

// Reports whether the announcement message contains the given address as a participant.
func (m *announcementMessage) Contains(addr address) bool {
	for _, adv := range m.Participants {
		if adv.Addr == addr {
			return true
		}
	}
	return false
}

// Returns the address of the participant with the highest ability.
func (m *announcementMessage) GetHighest() (address, Ability) {
	if len(m.Participants) == 0 {
		panic("no participants in advertisement")
	}
	max := m.Participants[0].Ability
	maxAddr := m.Participants[0].Addr
	for _, adv := range m.Participants {
		// In case of equal ability, prioritize the one with the higher address
		if adv.Ability > max || (adv.Ability == max && maxAddr.LessThan(adv.Addr)) {
			max = adv.Ability
			maxAddr = adv.Addr
		}
	}
	return maxAddr, max
}

// Returns a new announcement message with the given address and ability added as a participant.
func (m *announcementMessage) WithParticipant(addr address, ability Ability) announcementMessage {
	return announcementMessage{
		append(m.Participants, announcement{ability, addr}),
	}
}

func (m announcementMessage) String() string {
	str := "Adv{"
	for _, adv := range m.Participants {
		str += fmt.Sprintf("%v-%v, ", adv.Addr, adv.Ability)
	}
	return str + "}"
}

type resultMessage struct {
	Leader       address
	Participants []address
}

func startResult(leader address, self address) resultMessage {
	return resultMessage{leader, []address{self}}
}

func (resultMessage) RegisterToGob() {
	gob.Register(address{})
	gob.Register(resultMessage{})
}

// Reports whether the result message contains the given address as a participant.
func (m *resultMessage) Contains(addr address) bool {
	for _, p := range m.Participants {
		if p == addr {
			return true
		}
	}
	return false
}

// Returns a new result message with the given address added as a participant.
func (m *resultMessage) WithParticipant(addr address) resultMessage {
	return resultMessage{
		m.Leader,
		append(m.Participants, addr),
	}
}

func (m resultMessage) String() string {
	str := "Res{"
	str += fmt.Sprintf("leader: %v, [", m.Leader)
	for _, p := range m.Participants {
		str += fmt.Sprintf("%v, ", p)
	}
	return str + "]}"
}
