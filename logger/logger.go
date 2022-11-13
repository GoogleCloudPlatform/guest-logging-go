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
	"google.golang.org/api/option"
	mrpb "google.golang.org/genproto/googleapis/api/monitoredres"
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
	Writers   []io.Writer
	UserAgent string
	// metadata uses the GCE metadata server by default.
	metadata metadataProvider
}

// SetDebugLogging enables or disables debug level logging.
func SetDebugLogging(enabled bool) {
	debugEnabled = enabled
}

func cloudLoggerOpts(metadata metadataProvider) []logging.LoggerOption {
	if metadata.OnGKE() {
		return []logging.LoggerOption{logging.CommonResource(&mrpb.MonitoredResource{
			Type: "k8s_node",
			Labels: map[string]string{
				"cluster_name": metadata.K8sClusterName(),
				// Although there is a cluster-location instance attribute in GKE nodes, it may be useful to know the actual zone of this node.
				"location":   metadata.Zone(),
				"node_name":  metadata.InstanceName(),
				"project_id": metadata.ProjectID(),
			},
		})}
	} else if !metadata.OnGCE() {
		// Who knows where this is running. Let Cloud Logging figure out the correct monitored resource type, if it can.
		return []logging.LoggerOption{}
	}
	var opts []logging.LoggerOption
	// The gce_instance resource type has instance_id, but not the name of the instance.
	// To faciliate queries, we add an instance_name label to all log Entries.
	name := metadata.InstanceName()
	if name != "" {
		opts = append(opts, logging.CommonLabels(map[string]string{"instance_name": name}))
	}
	opts = append(opts, logging.CommonResource(&mrpb.MonitoredResource{
		Type: "gce_instance",
		Labels: map[string]string{
			"instance_id": metadata.InstanceID(),
			"project_id":  metadata.ProjectID(),
			"zone":        metadata.Zone(),
		},
	}))
	return opts
}

// Init instantiates the logger.
func Init(ctx context.Context, opts LogOpts) error {
	if opts.LoggerName == "" {
		return fmt.Errorf("logger name must be set")
	}
	if opts.metadata == nil {
		opts.metadata = defaultGCEMetadataProvider
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

		clOpts := cloudLoggerOpts(opts.metadata)
		cloudLogger = cloudLoggingClient.Logger(loggerName, clOpts...)

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
func Close() error {
	var errs []error
	if cloudLoggingClient != nil {
		if err := cloudLoggingClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("Failed to close Cloud Logging client: %v", err))
		}
	}
	if err := localClose(); err != nil {
		errs = append(errs, fmt.Errorf("Failed to close local client: %v", err))
	}
	if errs != nil {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
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
