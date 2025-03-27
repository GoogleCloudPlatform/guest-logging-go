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
	"regexp"
	"time"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/logging"
	"google.golang.org/api/option"
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

const (
	// The following are MIG labels.
	migNameLabel   = `compute.googleapis.com/instance_group_manager/name`
	migZoneLabel   = `compute.googleapis.com/instance_group_manager/zone`
	migRegionLabel = `compute.googleapis.com/instance_group_manager/region`
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
	Writers   []io.Writer
	UserAgent string
	// MIG is the Managed Instance Group, used for labeling logs.
	MIG string
}

// SetDebugLogging enables or disables debug level logging.
func SetDebugLogging(enabled bool) {
	debugEnabled = enabled
}

// parseMIGLabels parses the given MIG string into a map of labels for
// Cloud Logging.
func parseMIGLabels(mig string) map[string]string {
	labels := make(map[string]string)
	if mig == "" {
		return labels
	}

	migRe := regexp.MustCompile(`^projects/[^/]+/(zones|regions)/([^/]+)/instanceGroupManagers/([^/]+)$`)
	matches := migRe.FindStringSubmatch(mig)
	if matches == nil {
		return labels
	}

	var locationLabel string
	switch matches[1] {
	case "zones":
		locationLabel = migZoneLabel
	case "regions":
		locationLabel = migRegionLabel
	}
	labels[migNameLabel] = matches[3]
	labels[locationLabel] = matches[2]
	return labels
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
		cOpts := []option.ClientOption{}
		if opts.UserAgent != "" {
			cOpts = append(cOpts, option.WithUserAgent(opts.UserAgent))
		}
		cloudLoggingClient, err = logging.NewClient(ctx, opts.ProjectName, cOpts...)
		if err != nil {
			Errorf("Continuing without cloud logging due to error in initialization: %v", err.Error())
			// Log but don't return this error, as it doesn't prevent continuing.
			return nil
		}

		// Override default error handler. Must be a func and not nil.
		cloudLoggingClient.OnError = func(e error) { return }

		// The logger automatically detects and associates with a GCE
		// resource. However instance_name is not included in this
		// resource, so add an instance_name label to all log Entries.
		name, err := metadata.InstanceName()
		labels := make(map[string]string)
		if err == nil {
			labels["instance_name"] = name
		}

		// Add MIG labels if provided.
		migLabels := parseMIGLabels(opts.MIG)
		for k, v := range migLabels {
			labels[k] = v
		}

		// Initialize the logger.
		cloudLogger = cloudLoggingClient.Logger(loggerName, logging.CommonLabels(labels))

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
		// Attempt to connect to Cloud Logging.
		timeoutContext, cancelFunc := context.WithTimeout(context.Background(), time.Second*3)
		defer cancelFunc()

		if err := cloudLoggingClient.Ping(timeoutContext); err != nil {
			Warningf("Cannot connect to cloud logging, skipping flush: %v", err)
		} else {
			cloudLogger.Flush()
			cloudLoggingClient.Close()
		}
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
		var payload interface{}
		if e.StructuredPayload != nil {
			payload = e.StructuredPayload
		} else {
			payload = e
		}
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
		cloudLogger.Log(logging.Entry{Severity: cloudSev, SourceLocation: e.Source, Payload: payload, Labels: e.Labels})
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
