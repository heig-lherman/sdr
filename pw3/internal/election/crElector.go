package election

import (
	"chatsapp/internal/election/ring"
	"chatsapp/internal/logging"
	"chatsapp/internal/server/dispatcher"
	"chatsapp/internal/transport"
)

type Ability = int

type address = transport.Address

// Interface for an elector, which is responsible for electing a leader among a group of processes, based on their abilities.
//
// It garantees that all processes will eventually agree on the same leader, and that the leader will be the one with the highest ability.
type Elector interface {
	// Returns the current leader of the group. May be blocking if an election is ongoing or required to determine the leader.
	GetLeader() address
	// Updates the ability of this process, and starts a new election. May be blocking if an election is ongoing.
	UpdateAbility(ability Ability)
}

type newAbilityRequest struct {
	ability Ability
	result  chan struct{}
}

// Local implementation of an elector, based on the Chang-Roberts algorithm.
type crElector struct {
	log *logging.Logger

	self address
	ring ring.RingMaintainer

	getLeader  chan chan<- address
	newAbility chan newAbilityRequest

	incAnnouncement chan announcementMessage
	incResult       chan resultMessage
}

/*
Constructs and returns a new Chang-Roberts elector.

Parameters
  - logger: The logger to use for logging messages.
  - self: The address of the current process.
  - dispatcher: The dispatcher to use for sending messages to processes in the network.
  - ringAddrs: A slice of addresses, the order of which represents the ring. Note that [self] *must* appear in the slice, though it may appear in any position.

Returns:
  - A new Chang-Roberts elector instance.

Note that the elector will start running in the background as soon as it is created.
*/
func NewCRElector(
	logger *logging.Logger,
	self address,
	dispatcher dispatcher.Dispatcher,
	ringAddrs []address,
) Elector {
	return newCRElector(
		logger,
		self,
		ring.NewRingMaintainer(
			logger.WithPostfix("ring"),
			dispatcher,
			self,
			ringAddrs,
		),
	)
}

// Creates a new crElector instance. This function is used to facilitate testing.
func newCRElector(
	logger *logging.Logger,
	self address,
	ring ring.RingMaintainer,
) Elector {
	announcementMessage{}.RegisterToGob()
	resultMessage{}.RegisterToGob()

	cre := &crElector{
		log:             logger,
		self:            self,
		ring:            ring,
		getLeader:       make(chan chan<- address),
		newAbility:      make(chan newAbilityRequest),
		incAnnouncement: make(chan announcementMessage),
		incResult:       make(chan resultMessage),
	}

	go cre.handleRingMessages()
	go cre.processElections()

	return cre
}

// GetLeader blocks if an election is ongoing, then returns the current leader of the group.
func (cre *crElector) GetLeader() address {
	res := make(chan address)
	cre.getLeader <- res
	return <-res
}

// UpdateAbility updates the ability of this process, and starts a new election.
func (cre *crElector) UpdateAbility(ability Ability) {
	wait := make(chan struct{})
	cre.newAbility <- newAbilityRequest{ability, wait}
	<-wait
}

// handleRingMessages listens for incoming messages from the ring and dispatches them to the appropriate handlers.
func (cre *crElector) handleRingMessages() {
	for {
		switch msg := cre.ring.ReceiveFromPrev().(type) {
		case announcementMessage:
			cre.log.Infof("Received announcement message: %v", msg)
			cre.incAnnouncement <- msg
		case resultMessage:
			cre.log.Infof("Received result message: %v", msg)
			cre.incResult <- msg
		default:
			cre.log.Warnf("Received unexpected message type %T from ring, ignoring", msg)
		}
	}
}

// processElections is the main goroutine of the elector, it manages the inner state
// and handles incoming requests from the application-facing API and the ring
func (cre *crElector) processElections() {
	leader := new(address)
	ability := 0
	inElection := false

	electionWaiters := make([]func(leader address), 0)

	// TODO ask if alright to do closures, or should do state struct with methods
	// Define some helpful closures for the election process
	startElection := func() {
		inElection = true
		cre.log.Infof("Starting new election with ability %v", ability)
		cre.ring.SendToNext(startAnnouncement(ability, cre.self))
	}

	endElection := func() {
		inElection = false
		cre.log.Infof("Election completed. Leader is now %v with %d waiters to notify", *leader, len(electionWaiters))

		// As an addition to the algorithm, we notify all
		// the election waiters of the newly acquired result
		for _, waiter := range electionWaiters {
			go waiter(*leader)
		}

		electionWaiters = electionWaiters[:0]
	}

	for {
		select {
		// 1. Election requests, through ability updates
		case newAbility := <-cre.newAbility:
			// 1.a. If an election is ongoing, reschedule the request after the election
			if inElection {
				cre.log.Infof("Received ability update to %v during election, rescheduling after completion", newAbility.ability)
				electionWaiters = append(electionWaiters, func(_ address) { cre.newAbility <- newAbility })
				continue
			}

			// 1.b. Update the ability
			cre.log.Infof("Updating ability from %v to %v and starting new election", ability, newAbility.ability)
			ability = newAbility.ability
			newAbility.result <- struct{}{}

			// 1.c. Start a new election
			startElection()

		// 2. Leader requests from the applicative side
		case res := <-cre.getLeader:
			// If we have no leader, we start an election now
			if !inElection && *leader == (address{}) {
				cre.log.Info("No leader exists, initiating election in response to leader request")
				startElection()
			}

			// If we are not in an election, we return the leader immediately,
			// otherwise we add the requester to the waiters list
			if !inElection {
				cre.log.Infof("Returning current leader %v to requester", *leader)
				res <- *leader
			} else {
				cre.log.Info("Election in progress, queuing leader request")
				electionWaiters = append(electionWaiters, func(leader address) { res <- leader })
			}

		// 3. When receiving an announcement message
		case ann := <-cre.incAnnouncement:
			// 3.a. If self is in the participants list
			if ann.Contains(cre.self) {
				// We have gone through a full round of the ring
				// 3.a.i.   The leader is the participant with the highest ability
				newLeader, highestAbility := ann.GetHighest()
				cre.log.Infof("Announcement completed ring traversal. Highest ability is %v from %v", highestAbility, newLeader)
				*leader = newLeader
				// 3.a.ii.  Start a result loop with the leader and us as participant
				cre.ring.SendToNext(startResult(*leader, cre.self))
				// 3.a.iii. Signal that the election is over
				endElection()
			} else {
				// 3.b.i.   Add ourselves in the participants
				// 3.b.ii.  Forward the message to the next node in the ring
				cre.log.Infof("Adding self to announcement with ability %v", ability)
				cre.ring.SendToNext(ann.WithParticipant(cre.self, ability))
				// 3.b.iii. Ensure the election state is up to date
				inElection = true
			}

		// 4. When receiving a result message
		case res := <-cre.incResult:
			// 4.a. If self is in the participants list, ignore the message
			if res.Contains(cre.self) {
				cre.log.Info("Ignoring result message we've already seen")
				continue
			}

			// 4.b. If we are not in an election, and the result leader is new
			if !inElection && *leader != res.Leader {
				// 4.b.i. We need to start a new election
				cre.log.Infof("Received result with new leader %v while not in election, starting new election", res.Leader)
				startElection()
			} else {
				// We can accept the result
				// 4.c.i.   Update the leader
				cre.log.Infof("Updating leader to %v and propagating result", res.Leader)
				*leader = res.Leader
				// 4.c.ii.  Add ourselves in the participants
				// 4.c.iii. Forward the message to the next node in the ring
				cre.ring.SendToNext(res.WithParticipant(cre.self))
				// 4.c.iv.  Signal that the election is over
				endElection()
			}
		}
	}
}
