package logging

import (
	"log"
	"os"
)

// Describes a file that can be written to by a loggerg
type LogFile struct {
	channel chan string
	file    *os.File
}

func NewLogFile(path string) *LogFile {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatal(err)
	}

	lf := LogFile{
		channel: make(chan string, 100),
		file:    file,
	}

	go func() {
		defer file.Close()
		for s := range lf.channel {
			lf.file.WriteString(s)
		}
	}()

	return &lf
}

func (lf *LogFile) Print(s string) {
	lf.channel <- s
}
