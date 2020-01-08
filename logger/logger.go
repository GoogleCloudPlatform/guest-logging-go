//  Copyright 2019 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Package logger logs messages as appropriate.
package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"cloud.google.com/go/logging"
)

var (
	// DeferredFatalFuncs is a slice of functions that will be called prior to os.Exit in Fatal.
	DeferredFatalFuncs []func()

	cloudLoggingClient *logging.Client
	cloudLogger        *logging.Logger
	debugEnabled       bool
	loggerName         string
	formatFunction     func(LogEntry) string

	writers []io.Writer
)

// LogOpts represents options for logging.
type LogOpts struct {
	Debug               bool
	ProjectName         string
	LoggerName          string
	DisableLocalLogging bool
	DisableCloudLogging bool
	// FormatFunction will produce the string representation of each log event.
	FormatFunction func(LogEntry) string
	// Additional writers that will be used during logging.
	Writers []io.Writer
}

// Init instantiates the logger.
func Init(ctx context.Context, opts LogOpts) error {
	if opts.LoggerName == "" {
		return fmt.Errorf("logger name must be set")
	}

	loggerName = opts.LoggerName
	debugEnabled = opts.Debug
	formatFunction = opts.FormatFunction
	writers = opts.Writers

	if !opts.DisableLocalLogging {
		if err := localSetup(loggerName); err != nil {
			return fmt.Errorf("logger Init localSetup error: %v", err)
		}
	}

	if !opts.DisableCloudLogging && opts.ProjectName != "" {
		var err error
		cloudLoggingClient, err = logging.NewClient(ctx, opts.ProjectName)
		if err != nil {
			Errorf("Continuing without cloud logging due to error in initialization: %v", err.Error())
			// Log but don't return this error, as it doesn't prevent continuing.
			return nil
		}

		// This automatically detects and associates with a GCE resource.
		cloudLogger = cloudLoggingClient.Logger(loggerName)

		go func() {
			for {
				time.Sleep(5 * time.Second)
				cloudLogger.Flush()
			}
		}()
	}

	return nil
}

// Close closes the logger.
func Close() {
	if cloudLoggingClient != nil {
		cloudLoggingClient.Close()
	}
	localClose()
}

// Log writes an entry to all outputs.
func Log(e LogEntry) {
	if e.Severity == Debug && !debugEnabled {
		return
	}
	if e.CallDepth == 0 {
		e.CallDepth = 2
	}
	e.LocalTimestamp = now()
	e.Source = caller(e.CallDepth)
	local(e)
	for _, w := range writers {
		w.Write(e.bytes())
	}

	var cloudSev logging.Severity
	if cloudLogger != nil {
		switch e.Severity {
		case Debug:
			cloudSev = logging.Debug
		case Info:
			cloudSev = logging.Info
		case Warning:
			cloudSev = logging.Warning
		case Error:
			cloudSev = logging.Error
		case Critical:
			cloudSev = logging.Critical
		default:
			cloudSev = logging.Default
		}
		cloudLogger.Log(logging.Entry{Severity: cloudSev, SourceLocation: e.Source, Payload: e, Labels: e.Labels})
	}
}

// Debugf logs debug information.
func Debugf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: Debug})
}

// Infof logs general information.
func Infof(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: Info})
}

// Warningf logs warning information.
func Warningf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: Warning})
}

// Errorf logs error information.
func Errorf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: Error})
}

// Fatalf logs critical error information and exits.
func Fatalf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: Critical})

	for _, f := range DeferredFatalFuncs {
		f()
	}
	Close()
	os.Exit(1)
}
