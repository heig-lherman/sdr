package ioUtils

import "fmt"

// A mock implementation of the IOStream interface, useful for testing.
type MockIOStream interface {
	IOStream
	// Provide the next line of input that will be read by the stream.
	SimulateNextInputLine(string)
	// Retrieve the next line that will be written to the stream.
	InterceptNextPrintln() string
}

type mockReader struct {
	nextReadLine    chan string
	nextWrittenLine chan string
}

func NewMockReader() MockIOStream {
	reader := mockReader{
		nextReadLine:    make(chan string, 1000),
		nextWrittenLine: make(chan string, 1000),
	}
	return reader
}

func (m mockReader) ReadLine() (string, error) {
	s := <-m.nextReadLine
	return s, nil
}

func (m mockReader) Println(s ...interface{}) {
	str := fmt.Sprint(s...)
	m.Print(str, "\n")
}

func (m mockReader) Print(s ...interface{}) {
	str := fmt.Sprint(s...)
	m.nextWrittenLine <- str
	fmt.Print("MOCK: " + str)
}

func (m mockReader) SimulateNextInputLine(s string) {
	m.nextReadLine <- s
}

func (m mockReader) InterceptNextPrintln() string {
	return <-m.nextWrittenLine
}
