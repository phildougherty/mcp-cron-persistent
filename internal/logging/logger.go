// SPDX-License-Identifier: AGPL-3.0-only
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	// Debug level for verbose debugging information
	Debug LogLevel = iota
	// Info level for general information about the application
	Info
	// Warn level for warnings
	Warn
	// Error level for errors
	Error
	// Fatal level for fatal errors
	Fatal
)

// Logger provides enhanced logging functionality
type Logger struct {
	logger *log.Logger
	level  LogLevel
	fields map[string]string
}

// Options configures the logger behavior
type Options struct {
	// Output destination (defaults to os.Stdout)
	Output io.Writer
	// Minimum log level (defaults to Info)
	Level LogLevel
	// Optional prefix for log messages
	Prefix string
	// Default fields to include in every log message
	DefaultFields map[string]string
}

// DefaultOptions returns the default logger options
func DefaultOptions() Options {
	return Options{
		Output: os.Stdout,
		Level:  Info,
		Prefix: "",
	}
}

// New creates a new logger with the specified options
func New(options Options) *Logger {
	if options.Output == nil {
		options.Output = os.Stdout
	}

	if options.DefaultFields == nil {
		options.DefaultFields = make(map[string]string)
	}

	return &Logger{
		logger: log.New(options.Output, options.Prefix, log.LstdFlags),
		level:  options.Level,
		fields: options.DefaultFields,
	}
}

// FileLogger creates a logger that writes to a file
func FileLogger(path string, level LogLevel) (*Logger, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	return New(Options{
		Output: file,
		Level:  level,
	}), nil
}

// WithField returns a new logger with an additional field
func (l *Logger) WithField(key, value string) *Logger {
	// Create a copy of the logger
	newLogger := &Logger{
		logger: l.logger,
		level:  l.level,
		fields: make(map[string]string, len(l.fields)+1),
	}

	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}

	// Add new field
	newLogger.fields[key] = value

	return newLogger
}

// Debugf logs a debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	if l.level <= Debug {
		l.logf("DEBUG", format, args...)
	}
}

// Infof logs an info message
func (l *Logger) Infof(format string, args ...interface{}) {
	if l.level <= Info {
		l.logf("INFO", format, args...)
	}
}

// Warnf logs a warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	if l.level <= Warn {
		l.logf("WARN", format, args...)
	}
}

// Errorf logs an error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	if l.level <= Error {
		l.logf("ERROR", format, args...)
	}
}

// Fatalf logs a fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	if l.level <= Fatal {
		l.logf("FATAL", format, args...)
		os.Exit(1)
	}
}

// logf formats and logs a message with the specified level
func (l *Logger) logf(level, format string, args ...interface{}) {
	now := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)

	// Format fields
	fields := ""
	if len(l.fields) > 0 {
		fields = " ["
		first := true
		for k, v := range l.fields {
			if !first {
				fields += ", "
			}
			fields += fmt.Sprintf("%s=%s", k, v)
			first = false
		}
		fields += "]"
	}

	l.logger.Printf("[%s] %s:%s %s", level, now, fields, msg)
}

// Global default logger
var defaultLogger = New(DefaultOptions())

// SetDefaultLogger sets the global default logger
func SetDefaultLogger(logger *Logger) {
	defaultLogger = logger
}

// GetDefaultLogger returns the global default logger
func GetDefaultLogger() *Logger {
	return defaultLogger
}
