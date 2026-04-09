package logging

import (
	"io"
	"log"
	"os"
)

const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

type Logger struct {
	level string

	consoleInfo  *log.Logger
	consoleWarn  *log.Logger
	consoleError *log.Logger
	consoleDebug *log.Logger

	fileInfo  *log.Logger
	fileWarn  *log.Logger
	fileError *log.Logger
	fileDebug *log.Logger
}

func New(level string, console io.Writer, file io.Writer) *Logger {
	flags := log.Ldate | log.Ltime | log.Lmicroseconds
	useColor := shouldColorizeConsole()
	l := &Logger{level: level}

	l.consoleInfo = log.New(console, levelPrefix("INFO", useColor), flags)
	l.consoleWarn = log.New(console, levelPrefix("WARN", useColor), flags)
	l.consoleError = log.New(console, levelPrefix("ERROR", useColor), flags)
	l.consoleDebug = log.New(console, levelPrefix("DEBUG", useColor), flags)

	if file != nil {
		l.fileInfo = log.New(file, levelPrefix("INFO", false), flags)
		l.fileWarn = log.New(file, levelPrefix("WARN", false), flags)
		l.fileError = log.New(file, levelPrefix("ERROR", false), flags)
		l.fileDebug = log.New(file, levelPrefix("DEBUG", false), flags)
	}

	return l
}

func NewFromConfig(level string, logFile string) (*Logger, error) {
	if logFile == "" {
		return New(level, os.Stdout, nil), nil
	}

	output, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	return New(level, os.Stdout, output), nil
}

func (l *Logger) Debug(format string, v ...interface{}) {
	if l.level == LevelDebug {
		l.consoleDebug.Printf(format, v...)
		if l.fileDebug != nil {
			l.fileDebug.Printf(format, v...)
		}
	}
}

func (l *Logger) Info(format string, v ...interface{}) {
	if l.level == LevelDebug || l.level == LevelInfo {
		l.consoleInfo.Printf(format, v...)
		if l.fileInfo != nil {
			l.fileInfo.Printf(format, v...)
		}
	}
}

func (l *Logger) Warn(format string, v ...interface{}) {
	if l.level != LevelError {
		l.consoleWarn.Printf(format, v...)
		if l.fileWarn != nil {
			l.fileWarn.Printf(format, v...)
		}
	}
}

func (l *Logger) Error(format string, v ...interface{}) {
	l.consoleError.Printf(format, v...)
	if l.fileError != nil {
		l.fileError.Printf(format, v...)
	}
}

func shouldColorizeConsole() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR") == "0" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func levelPrefix(level string, colored bool) string {
	switch level {
	case "DEBUG":
		if colored {
			return "\x1b[36m[DEBUG]\x1b[0m "
		}
		return "[DEBUG] "
	case "INFO":
		if colored {
			return "\x1b[32m[INFO]\x1b[0m  "
		}
		return "[INFO]  "
	case "WARN":
		if colored {
			return "\x1b[33m[WARN]\x1b[0m  "
		}
		return "[WARN]  "
	case "ERROR":
		if colored {
			return "\x1b[31m[ERROR]\x1b[0m "
		}
		return "[ERROR] "
	default:
		if colored {
			return "\x1b[90m[LOG]\x1b[0m   "
		}
		return "[LOG]   "
	}
}
