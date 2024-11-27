package ioUtils

import (
	"bufio"
	"fmt"
	"os"
)

type stdStream struct {
	ioBuffer bufio.Reader
}

// NewStdStream creates a new instance of an IOStream that pipes to the standard input/output.
func NewStdStream() IOStream {
	return stdStream{
		ioBuffer: *bufio.NewReader(os.Stdin),
	}
}

func (s stdStream) ReadLine() (string, error) {
	line, err := s.ioBuffer.ReadString('\n')
	if err == nil && len(line) > 0 {
		line = line[:len(line)-1]
	}
	return line, err
}

func (stream stdStream) Println(s ...interface{}) {
	str := fmt.Sprintln(s...)
	stream.Print(str)
}

func (stdStream) Print(s ...interface{}) {
	fmt.Print(s...)
}
