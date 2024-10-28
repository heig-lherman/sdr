package tcp

type handlerRegistration struct {
	id      HandlerId
	handler MessageHandler
}

func (tcp *TCP) handleHandlers() {
	registeredHandlers := make(map[HandlerId]MessageHandler)

	for {
		select {
		case msg := <-tcp.receivedMessages:
			tcp.handleReceive(msg, registeredHandlers)
		case registration := <-tcp.handlerRegistrations:
			tcp.logger.Infof("TCP registering handler %d", registration.id)
			registeredHandlers[registration.id] = registration.handler
		case id := <-tcp.handlerUnregistrations:
			tcp.logger.Infof("TCP unregistering handler %d", id)
			delete(registeredHandlers, id)
		case <-tcp.closeChan:
			return
		}
	}
}

func (tcp *TCP) handleReceive(
	msg Message,
	registeredHandlers map[HandlerId]MessageHandler,
) {
	tcp.logger.Info("TCP received message from", msg.Source, ". Dispatching it among ", len(registeredHandlers), " handlers.")
	for _, handler := range registeredHandlers {
		if handler.HandleNetworkMessage(&msg) {
			return
		}
	}
}
