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
	"cloud.google.com/go/compute/metadata"
)

type metadataProvider interface {
	K8sClusterName() string
	InstanceName() string
	Zone() string
	ProjectID() string
	InstanceID() string
	OnGCE() bool
	OnGKE() bool
}

type gceMetadataProvider struct{}

var defaultGCEMetadataProvider *gceMetadataProvider = &gceMetadataProvider{}

func (m *gceMetadataProvider) K8sClusterName() string {
	cluster, err := metadata.InstanceAttributeValue("cluster-name")
	if err != nil {
		return ""
	}
	return cluster
}

func (m *gceMetadataProvider) OnGKE() bool {
	return m.K8sClusterName() != ""
}

func (m *gceMetadataProvider) OnGCE() bool {
	return metadata.OnGCE()
}

func (m *gceMetadataProvider) InstanceName() string {
	name, err := metadata.InstanceName()
	if err != nil {
		return ""
	}
	return name
}

func (m *gceMetadataProvider) InstanceID() string {
	id, err := metadata.InstanceID()
	if err != nil {
		return ""
	}
	return id
}

func (m *gceMetadataProvider) Zone() string {
	zone, err := metadata.Zone()
	if err != nil {
		return ""
	}
	return zone
}

func (m *gceMetadataProvider) ProjectID() string {
	project, err := metadata.ProjectID()
	if err != nil {
		return ""
	}
	return project
}
