package main

import (
	"chatsapp/internal/server"
	"fmt"
	"log"
	"os"
)

func main() {
	conf, err := server.NewServerConfig(os.Args[1:])
	if err != nil {
		log.Fatal("Failed to create server config:", err)
	} else {
		fmt.Println("Server config created successfully:", conf)
	}

	fmt.Println("Hello, World!")

	s := server.NewServer(conf)
	s.Start()
}
