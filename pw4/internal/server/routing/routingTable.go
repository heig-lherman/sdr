package routing

import (
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/utils/option"
)

// The routing table indicates the next hop for each known address in the network,
// if we have no direct connection to a neighbor, the message is sent with proper metadata to the next neighbor who
// can forward it to the destination.
type routingTable = map[transport.Address]transport.Address

// The routing table handler holds a single instance and handles requests to it, ensuring that the table is accessed
// in a thread-safe manner. It starts a single goroutine when constructed.
type routingTableHandler struct {
	log *logging.Logger

	getReq    chan<- getTableReq
	lookupReq chan<- lookupTableReq
	updateReq chan<- updateTableReq
}

type getTableReq struct {
	res chan<- routingTable
}

type lookupTableReq struct {
	addr transport.Address
	res  chan<- option.Option[transport.Address]
}

type updateTableReq struct {
	table routingTable
	res   chan<- struct{}
}

func newRoutingTableHandler(
	log *logging.Logger,
	table routingTable,
) *routingTableHandler {
	getReq := make(chan getTableReq)
	lookupReq := make(chan lookupTableReq)
	updateReq := make(chan updateTableReq)

	h := &routingTableHandler{
		log:       log.WithPostfix("table"),
		getReq:    getReq,
		lookupReq: lookupReq,
		updateReq: updateReq,
	}

	go h.handleTable(table, getReq, lookupReq, updateReq)
	return h
}

// GetTable gets the routing table, blocks until the table is retrieved.
func (h *routingTableHandler) GetTable() routingTable {
	res := make(chan routingTable)
	h.getReq <- getTableReq{res: res}
	return <-res
}

// LookupTable looks up the next hop for the given address, blocks until the lookup is complete.
func (h *routingTableHandler) LookupTable(addr transport.Address) option.Option[transport.Address] {
	res := make(chan option.Option[transport.Address])
	h.lookupReq <- lookupTableReq{addr: addr, res: res}
	return <-res
}

// UpdateTable updates the routing table with the given table, blocks until the update is complete.
func (h *routingTableHandler) UpdateTable(table routingTable) {
	res := make(chan struct{})
	h.updateReq <- updateTableReq{table: table, res: res}
	<-res
}

func (h *routingTableHandler) handleTable(
	initial routingTable,
	getReq <-chan getTableReq,
	lookupReq <-chan lookupTableReq,
	updateReq <-chan updateTableReq,
) {
	table := make(routingTable)
	for k, v := range initial {
		table[k] = v
	}

	for {
		select {
		case req := <-getReq:
			h.log.Infof("Routing table requested")
			clone := make(routingTable)
			for k, v := range table {
				clone[k] = v
			}

			req.res <- clone

		case req := <-lookupReq:
			h.log.Infof("Routing table lookup requested")
			if next, ok := table[req.addr]; ok {
				req.res <- option.Some(next)
			} else {
				req.res <- option.None[transport.Address]()
			}

		case req := <-updateReq:
			h.log.Infof("Routing table update requested")
			for k, v := range req.table {
				table[k] = v
			}

			req.res <- struct{}{}
		}
	}
}
