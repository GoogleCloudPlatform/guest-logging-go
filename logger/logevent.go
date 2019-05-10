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
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cloud.google.com/go/logging"

	logpb "google.golang.org/genproto/googleapis/logging/v2"
)

// LogEntry encapsulates a single log entry.
type LogEntry struct {
	Message   string            `json:"message"`
	Labels    map[string]string `json:"-"`
	CallDepth int               `json:"-"`
	Severity  logging.Severity  `json:"-"`
}

type logEntry struct {
	LogEntry

	// annotate message and localTimestamp for payload use.
	localTimestamp string `json:"localTimestamp"`
	source         *logpb.LogEntrySourceLocation
}

func (e logEntry) String() string {
	if e.Severity == logging.Error || e.Severity == logging.Critical {
		// 2006-01-02T15:04:05.999999Z07:00 ERROR file.go:82: This is a log message.
		return fmt.Sprintf("%s %s %s:%d: %s", e.localTimestamp, e.Severity, e.source.File, e.source.Line, e.Message)
	}
	// 2006-01-02T15:04:05.999999Z07:00 INFO: This is a log message.
	return fmt.Sprintf("%s %s: %s", e.localTimestamp, e.Severity, e.Message)
}

func (e logEntry) Bytes() []byte {
	return []byte(strings.TrimSpace(e.String()) + "\n")
}

func now() string {
	// RFC3339 with microseconds.
	return time.Now().Format("2006-01-02T15:04:05.999999Z07:00")
}

func caller(depth int) *logpb.LogEntrySourceLocation {
	depth = depth + 1
	pc, file, line, ok := runtime.Caller(depth)
	if !ok {
		file = "???"
		line = 0
	}

	return &logpb.LogEntrySourceLocation{File: filepath.Base(file), Line: int64(line), Function: runtime.FuncForPC(pc).Name()}
}
