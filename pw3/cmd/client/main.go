package main

import (
	"chatsapp/internal/client"
	"chatsapp/internal/logging"
	"chatsapp/internal/transport"
	"chatsapp/internal/transport/tcp"
	"chatsapp/internal/utils/ioUtils"
	"fmt"
	"os"
)

func main() {
	logger := logging.NewLogger(ioUtils.NewStdStream(), nil, "main", false)

	if len(os.Args) < 4 {
		fmt.Println("Usage: <username> <local_address> <server_address>")
		return
	}

	username := os.Args[1]
	localAddrStr := os.Args[2]
	serverAddrStr := os.Args[3]

	localAddr, err := transport.NewAddress(localAddrStr)
	if err != nil {
		logger.Error("Failed to parse local address:", err)
		return
	}
	serverAddr, err := transport.NewAddress(serverAddrStr)
	if err != nil {
		logger.Error("Failed to parse server address:", err)
		return
	}

	network := tcp.NewTCP(localAddr, logger.WithPostfix("tcp"))

	stream := ioUtils.NewStdStream()

	client := client.NewClient(
		logger.WithPostfix("client"),
		serverAddr,
		localAddr,
		client.Username(username),
		network,
		stream,
	)
	client.Run()
}
