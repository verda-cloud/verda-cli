// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package network

import (
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// networkView is the JSON/YAML shape for one private network.
type networkView struct {
	ID        string     `json:"id"                    yaml:"id"`
	Name      string     `json:"name"                  yaml:"name"`
	Mode      string     `json:"mode"                  yaml:"mode"`
	IsDefault bool       `json:"is_default"            yaml:"is_default"`
	CreatedAt *time.Time `json:"created_at,omitempty"  yaml:"created_at,omitempty"`
}

func newNetworkView(n *verda.PrivateNetwork) networkView {
	v := networkView{ID: n.ID, Name: n.Name, Mode: n.Mode, IsDefault: n.IsDefault}
	if !n.CreatedAt.IsZero() {
		created := n.CreatedAt
		v.CreatedAt = &created
	}
	return v
}

func newNetworkViews(networks []verda.PrivateNetwork) []networkView {
	views := make([]networkView, len(networks))
	for i := range networks {
		views[i] = newNetworkView(&networks[i])
	}
	return views
}

// attachedInstanceView is the JSON/YAML shape for the instance a subnet routes through.
type attachedInstanceView struct {
	ID        string  `json:"id"                   yaml:"id"`
	Hostname  string  `json:"hostname"             yaml:"hostname"`
	PrivateIP string  `json:"private_ip,omitempty" yaml:"private_ip,omitempty"`
	PublicIP  *string `json:"public_ip,omitempty"  yaml:"public_ip,omitempty"`
	Status    string  `json:"status,omitempty"     yaml:"status,omitempty"`
}

// subnetView is the JSON/YAML shape for one subnet. NetworkID is not in the SDK
// subnet; callers pass the network the subnet was read from.
type subnetView struct {
	ID                   string                `json:"id"                               yaml:"id"`
	NetworkID            string                `json:"network_id,omitempty"             yaml:"network_id,omitempty"`
	Name                 string                `json:"name"                             yaml:"name"`
	Location             string                `json:"location"                         yaml:"location"`
	CIDR                 string                `json:"cidr"                             yaml:"cidr"`
	IsDefault            bool                  `json:"is_default"                       yaml:"is_default"`
	UsedIPCount          int                   `json:"used_ip_count"                    yaml:"used_ip_count"`
	TotalIPCount         int                   `json:"total_ip_count"                   yaml:"total_ip_count"`
	DefaultRouteInstance *attachedInstanceView `json:"default_route_instance,omitempty" yaml:"default_route_instance,omitempty"`
	CreatedAt            *time.Time            `json:"created_at,omitempty"             yaml:"created_at,omitempty"`
}

func newSubnetView(networkID string, s *verda.Subnet) subnetView {
	v := subnetView{
		ID:           s.ID,
		NetworkID:    networkID,
		Name:         s.Name,
		Location:     s.Location.Code,
		CIDR:         s.CIDR,
		IsDefault:    s.IsDefault,
		UsedIPCount:  s.UsedIPCount,
		TotalIPCount: s.TotalIPCount,
	}
	if a := s.DefaultRouteInstance; a != nil {
		v.DefaultRouteInstance = &attachedInstanceView{ID: a.ID, Hostname: a.Hostname, PrivateIP: a.PrivateIP, PublicIP: a.PublicIP, Status: a.Status}
	}
	if !s.CreatedAt.IsZero() {
		created := s.CreatedAt
		v.CreatedAt = &created
	}
	return v
}
