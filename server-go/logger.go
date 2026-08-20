package main

import (
	"log"
	"os"
	"strings"
)

// LogLevel represents the logging level
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

var (
	currentLogLevel LogLevel
	logger          *log.Logger
)

func initLogger() {
	// Default to INFO level, DEBUG only in development
	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr == "" {
		if os.Getenv("NODE_ENV") == "development" {
			currentLogLevel = LogLevelDebug
		} else {
			currentLogLevel = LogLevelInfo
		}
	} else {
		switch strings.ToUpper(logLevelStr) {
		case "DEBUG":
			currentLogLevel = LogLevelDebug
		case "INFO":
			currentLogLevel = LogLevelInfo
		case "WARN":
			currentLogLevel = LogLevelWarn
		case "ERROR":
			currentLogLevel = LogLevelError
		default:
			currentLogLevel = LogLevelInfo
		}
	}

	logger = log.New(os.Stdout, "", log.LstdFlags)
}

func logDebug(format string, v ...interface{}) {
	if currentLogLevel <= LogLevelDebug {
		logger.Printf("[DEBUG] "+format, v...)
	}
}

func logInfo(format string, v ...interface{}) {
	if currentLogLevel <= LogLevelInfo {
		logger.Printf("[INFO] "+format, v...)
	}
}

func logWarn(format string, v ...interface{}) {
	if currentLogLevel <= LogLevelWarn {
		logger.Printf("[WARN] "+format, v...)
	}
}

func logError(format string, v ...interface{}) {
	if currentLogLevel <= LogLevelError {
		logger.Printf("[ERROR] "+format, v...)
	}
}

