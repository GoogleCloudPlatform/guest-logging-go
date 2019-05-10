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

// +build windows

// Package logger logs messages as appropriate.
package logger

import (
	"os"

	"cloud.google.com/go/logging"
	"github.com/tarm/serial"
	"golang.org/x/sys/windows/svc/eventlog"
)

const EID = 882

var (
	el   *eventlog.Log
	port *serial.Port
)

func localSetup(loggerName string) error {
	err := eventlog.InstallAsEventCreate(loggerName, eventlog.Info|eventlog.Warning|eventlog.Error)
	if err != nil {
		return err
	}

	el, err = eventlog.Open(loggerName)
	if err != nil {
		return err
	}

	port, err = serial.OpenPort(&serial.Config{Name: "COM1", Baud: 115200})
	return err
}

func localClose() {
	if el != nil {
		el.Close()
	}
	if port != nil {
		port.Close()
	}
}

func local(e logEntry) {
	if port != nil {
		port.Write(e.Bytes())
	}

	if el != nil {
		switch e.Severity {
		case logging.Debug:
			if debugEnabled {
				el.Info(EID, e.String())
			}
		case logging.Info:
			el.Info(EID, e.String())
		case logging.Warning:
			el.Warning(EID, e.String())
		case logging.Error, logging.Critical:
			el.Error(EID, e.String())
		}
	}
	if stdoutEnabled {
		if (e.Severity == logging.Debug) && !debugEnabled {
			return
		}
		os.Stdout.Write(e.Bytes())
	}
}
