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
	stdoutEnabled      bool
)

// LogOpts represents options for logging.
type LogOpts struct {
	Stdout      bool
	Debug       bool
	ProjectName string
	LoggerName  string
}

// Init instantiates the logger.
func Init(ctx context.Context, opts LogOpts) error {
	if opts.ProjectName == "" || opts.LoggerName == "" {
		err := "Project and Logger names must be set"
		Errorf(err)
		return fmt.Errorf(err)
	}

	debugEnabled = opts.Debug
	stdoutEnabled = opts.Stdout

	localSetup(opts.LoggerName)

	var err error
	cloudLoggingClient, err = logging.NewClient(ctx, opts.ProjectName)
	if err != nil {
		Errorf("Continuing without cloud logging due to error in initialization: %v", err.Error())
		return nil
	}

	// This automatically detects and associates with a GCE resource.
	cloudLogger = cloudLoggingClient.Logger(opts.LoggerName)

	go func() {
		for {
			time.Sleep(5 * time.Second)
			cloudLogger.Flush()
		}
	}()

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
	if e.CallDepth == 0 {
		e.CallDepth = 2
	}
	le := logEntry{LogEntry: e, localTimestamp: now(), source: caller(e.CallDepth)}
	local(le)

	if cloudLogger != nil {
		cloudLogger.Log(logging.Entry{Severity: le.Severity, SourceLocation: le.source, Payload: le, Labels: le.Labels})
	}
}

// Debugf logs debug information.
func Debugf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: logging.Debug})
}

// Infof logs general information.
func Infof(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: logging.Info})
}

// Warningf logs warning information.
func Warningf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: logging.Warning})
}

// Errorf logs error information.
func Errorf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: logging.Error})
}

// Fatalf logs critical error information and exits.
func Fatalf(format string, v ...interface{}) {
	Log(LogEntry{Message: fmt.Sprintf(format, v...), Severity: logging.Critical})

	for _, f := range DeferredFatalFuncs {
		f()
	}
	Close()
	os.Exit(1)
}
