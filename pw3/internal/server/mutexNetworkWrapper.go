package server

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/mutex"
	"chatsapp/internal/server/dispatcher"
	"chatsapp/internal/transport"
)

// Constructs a new [mutexMsgPassingWrapper] instance.
func newMutexNetworkWrapper(log *logging.Logger, d dispatcher.Dispatcher) mutex.NetWrapper {
	netToMutex := make(chan mutex.Message)
	mutexToNet := make(chan mutex.OutgoingMessage)

	wrapper := mutex.NetWrapper{
		IntoNet: mutexToNet,
		FromNet: netToMutex,
	}

	d.Register(mutex.Message{}, func(msg dispatcher.Message, source transport.Address) {
		if mutexMsg, ok := msg.(mutex.Message); ok {
			netToMutex <- mutexMsg
		} else {
			log.Error("Received a message that is not a mutex.Message")
		}
	})

	go func() {
		for msg := range mutexToNet {
			addr, err := transport.NewAddress(string(msg.Destination))
			if err != nil {
				log.Error("Error creating address from pid:", err)
				return
			}
			d.Send(msg.Message, addr)
		}
	}()

	return wrapper
}
