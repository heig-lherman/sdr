package server

import (
	"chatsapp/internal/transport"
	"encoding/json"
	"log"
	"os"
)

// Represents the JSON configuration file
type ConfigFile struct {
	Debug        bool
	LogPath      string
	Neighbors    []string
	PrintReadAck bool `json:"PrintReadAck,omitempty"`
	SlowdownMs   uint32
	Username     string `json:"Username,omitempty"`
}

// Represents the server configuration, parsed from the JSON configuration file
type ServerConfig struct {
	Debug          bool
	LogPath        string
	Addr           transport.Address
	ClientStrategy clientCommStrategy
	Neighbors      []transport.Address
	PrintReadAck   bool
	SlowdownMs     uint32
}

// Creates a new server configuration from the given arguments
func NewServerConfig(args []string) (*ServerConfig, error) {
	if len(args) < 2 {
		log.Fatal("Not enough arguments. Usage: <local_address> <config_file>")
	}

	addr := args[0]
	neighborsFile := args[1]

	// Read config file
	config, err := readConfigFile(neighborsFile)
	if err != nil {
		log.Fatalf("Failed to read config file %s. %v", neighborsFile, err)
		return nil, err
	}

	selfAddr, err := transport.NewAddress(addr)
	if err != nil {
		return nil, err
	}
	neighbors := make([]transport.Address, len(config.Neighbors))
	foundSelf := false
	for i, neighborStr := range config.Neighbors {
		neighbor, err := transport.NewAddress(neighborStr)
		if err != nil {
			return nil, err
		}
		if neighbor == selfAddr {
			foundSelf = true
		}
		neighbors[i] = neighbor
	}
	if !foundSelf {
		log.Fatalf("Self address %s not found in neighbors list %v", selfAddr, neighbors)
	}

	var clientCommStrategy clientCommStrategy
	if config.Username != "" {
		clientCommStrategy = newLocalClientCommStrategy(Username(config.Username))
	} else {
		clientCommStrategy = newRemoteClientCommStrategy()
	}

	return &ServerConfig{
		Debug:          config.Debug,
		Addr:           selfAddr,
		ClientStrategy: clientCommStrategy,
		LogPath:        config.LogPath,
		Neighbors:      neighbors,
		PrintReadAck:   config.PrintReadAck,
		SlowdownMs:     config.SlowdownMs,
	}, nil
}

// Reads the configuration file and returns the parsed configuration
func readConfigFile(filename string) (*ConfigFile, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var config ConfigFile
	err = decoder.Decode(&config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
