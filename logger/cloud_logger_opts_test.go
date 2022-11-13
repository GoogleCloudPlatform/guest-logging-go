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

package logger

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

type fakeGCEMetadata struct {
	isGKE        bool
	isGCE        bool
	instanceName string
	instanceID   string
	zone         string
	projectID    string
	clusterName  string
}

func (m *fakeGCEMetadata) OnGKE() bool {
	return m.isGKE
}

func (m *fakeGCEMetadata) OnGCE() bool {
	return m.isGCE
}

func (m *fakeGCEMetadata) InstanceName() string {
	return m.instanceName
}

func (m *fakeGCEMetadata) InstanceID() string {
	return m.instanceID
}

func (m *fakeGCEMetadata) Zone() string {
	return m.zone
}

func (m *fakeGCEMetadata) ProjectID() string {
	return m.projectID
}

func (m *fakeGCEMetadata) K8sClusterName() string {
	return m.clusterName
}

func fetchTestRunLogEntries(project, testRun string) ([]*logging.Entry, error) {
	c, err := logadmin.NewClient(context.Background(), fmt.Sprintf("projects/%s", project))
	if err != nil {
		return nil, err
	}
	it := c.Entries(context.Background(), logadmin.ProjectIDs([]string{os.Getenv("PROJECT_NAME")}),
		logadmin.Filter(fmt.Sprintf(`
			logName = "projects/%s/logs/guest-logging-go-end-to-end-test"
			labels."test-run" = "%s"
	`, project, testRun)))
	var entries []*logging.Entry
	for err != iterator.Done {
		e, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func TestCloudLoggerResourceTypeAndLabels(t *testing.T) {
	for _, tc := range []struct {
		description        string
		metadata           *fakeGCEMetadata
		wantResourceType   string
		wantResourceLabels map[string]string
		wantLogEntryLabels map[string]string
	}{
		{
			description: "GCE nodes that are not GKE use gce_instance",
			metadata: &fakeGCEMetadata{
				isGCE:        true,
				isGKE:        false,
				instanceID:   "42",
				instanceName: "HAL-9000",
				zone:         "antarctica-east1-b",
				projectID:    "pollos-fritos-staging",
			},
			wantResourceType: "gce_instance",
			wantResourceLabels: map[string]string{
				"instance_id": "42",
				"project_id":  "pollos-fritos-staging",
				"zone":        "antarctica-east1-b",
			},
			wantLogEntryLabels: map[string]string{
				"instance_name": "HAL-9000",
			},
		},
		{
			description: "GCE nodes that are GKE use k8s_node",
			metadata: &fakeGCEMetadata{
				isGKE:        true,
				isGCE:        true,
				instanceID:   "42",
				instanceName: "HAL-9000",
				zone:         "antarctica-east1-b",
				clusterName:  "secret-bioweapons-lab-prod-1",
				projectID:    "pollos-fritos-prod",
			},
			wantResourceType: "k8s_node",
			wantResourceLabels: map[string]string{
				"node_name":    "HAL-9000",
				"project_id":   "pollos-fritos-prod",
				"location":     "antarctica-east1-b",
				"cluster_name": "secret-bioweapons-lab-prod-1",
			},
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			// The test cases must run sequentially because cloudLogger and cloudLoggingClient are shared module-level variables.
			if err := Init(context.Background(), LogOpts{
				LoggerName:          "guest-logging-go-end-to-end-test",
				ProjectName:         os.Getenv("PROJECT_NAME"),
				DisableCloudLogging: false,
				DisableLocalLogging: true,
				// Some errors from the library may only show up on stdout.
				Writers:  []io.Writer{os.Stdout},
				metadata: tc.metadata,
			}); err != nil {
				t.Fatalf("Failed to initialize logger: %v", err)
			}
			testRun := uuid.New().String()
			Log(LogEntry{Message: "oy", Severity: Info, Labels: map[string]string{"test-run": testRun}})
			if err := Close(); err != nil {
				t.Fatalf("Failed to close logger: %v", err)
			}

			entries, err := fetchTestRunLogEntries(os.Getenv("PROJECT_NAME"), testRun)
			if err != nil {
				t.Fatalf("Failed to read log entries from Cloud Logging: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("Wanted 1 Cloud Logging log entry, but got %d: %v", len(entries), entries)
			}
			if entries[0].Resource == nil {
				t.Fatalf("Wanted a Cloud Logging entry with resource type %s, but got no resource in the log entry: %v", tc.wantResourceType, entries[0])
			}
			gotType := entries[0].Resource.Type
			if gotType != tc.wantResourceType {
				t.Errorf("Wanted Cloud Logging entry with resource type %s, but got %s: %v", tc.wantResourceType, gotType, entries[0])
			}
			gotLabels := entries[0].Resource.Labels
			if !cmp.Equal(gotLabels, tc.wantResourceLabels) {
				t.Errorf("Wanted Cloud Logging entry with resource labels %v, but got %v; diff = %v", tc.wantResourceLabels, gotLabels, cmp.Diff(tc.wantResourceLabels, gotLabels))
			}
			for key, wantValue := range tc.wantLogEntryLabels {
				gotValue, ok := entries[0].Labels[key]
				if !ok {
					t.Errorf("Wanted log entry to contain label %s = %s, but it wasn't present: %v", key, wantValue, entries[0].Labels)
					continue
				}
				if wantValue != gotValue {
					t.Errorf("wanted log entry to contain label %s = %s, but got value %s = %s", key, wantValue, key, gotValue)
				}
			}
		})
	}
}
