package logging

import (
	"chatsapp/internal/utils/ioUtils"
	"fmt"
)

// The logging level of a logger
type LogLevel uint8

const (
	// Lowest logging level; all logs are printed
	INFO LogLevel = 1
	// Middle logging level; only warnings and errors are printed
	WARN LogLevel = 2
	// Highest logging level; only errors are printed
	ERR LogLevel = 3
)

type Logger struct {
	ioStream ioUtils.IOStream
	file     *LogFile
	name     string
	logLevel LogLevel
	fileOnly bool
}

// Constructs and returns a new logger instance.
func NewLogger(ioStream ioUtils.IOStream, file *LogFile, name string, fileOnly bool) *Logger {
	return &Logger{
		ioStream: ioStream,
		file:     file,
		name:     name,
		fileOnly: fileOnly,
		logLevel: INFO,
	}
}

// Returns a new instance of a logger that logs to the standard output.
func NewStdLogger(name string) *Logger {
	return NewLogger(ioUtils.NewStdStream(), nil, name, false)
}

// Returns a new logger with the same configuration, but with the given log level.
func (l *Logger) WithLogLevel(level LogLevel) *Logger {
	return &Logger{
		ioStream: l.ioStream,
		file:     l.file,
		name:     l.name,
		logLevel: level,
		fileOnly: l.fileOnly,
	}
}

// Returns a new logger with the same configuration, but with the given postfix appended to the name.
func (l *Logger) WithPostfix(postfix string) *Logger {
	return &Logger{
		ioStream: l.ioStream,
		file:     l.file,
		name:     fmt.Sprintf("%s|%s", l.name, postfix),
		logLevel: l.logLevel,
		fileOnly: l.fileOnly,
	}
}

func (l *Logger) Info(args ...interface{}) {
	if l.logLevel > INFO {
		return
	}
	s := fmt.Sprintf("[INFO|%s] %s\n", l.name, fmt.Sprint(args...))
	if l.file != nil {
		l.file.Print(s)
	}
	if !l.fileOnly {
		// nl.ioStream.Print(s)
		fmt.Print(s)
	}
}

func (l *Logger) Infof(format string, args ...interface{}) {
	l.Info(fmt.Sprintf(format, args...))
}

func (l *Logger) Warn(args ...interface{}) {
	if l.logLevel > WARN {
		return
	}
	s := fmt.Sprintf("[WARN|%s] %s\n", l.name, fmt.Sprint(args...))
	if l.file != nil {
		l.file.Print(s)
	}
	if !l.fileOnly {
		// nl.ioStream.Print(s)
		fmt.Print(s)
	}
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.Warn(fmt.Sprintf(format, args...))
}

func (l *Logger) Error(args ...interface{}) {
	if l.logLevel > ERR {
		return
	}
	s := fmt.Sprintf("[ERROR|%s] %s\n", l.name, fmt.Sprint(args...))
	if l.file != nil {
		l.file.Print(s)
	}
	if !l.fileOnly {
		// nl.ioStream.Print(s)
		fmt.Print(s)
	}
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.Error(fmt.Sprintf(format, args...))
}
