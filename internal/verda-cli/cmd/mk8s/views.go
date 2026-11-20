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

package mk8s

import (
	"fmt"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// KubeconfigView is the JSON/YAML (and --debug) shape of a kubeconfig. It has
// no field for the YAML, which holds a client private key: never marshal the
// SDK struct instead.
//
// Every key is always present, so scripts see one shape (a pending kubeconfig
// has not_after null, one without groups has []); only file is conditional.
type KubeconfigView struct {
	ID          string     `json:"id"              yaml:"id"`
	DisplayName string     `json:"display_name"    yaml:"display_name"`
	CommonName  string     `json:"common_name"     yaml:"common_name"`
	Groups      []string   `json:"groups"          yaml:"groups"`
	Status      string     `json:"status"          yaml:"status"`
	NotAfter    *time.Time `json:"not_after"       yaml:"not_after"`
	File        string     `json:"file,omitempty"  yaml:"file,omitempty"`
}

// newKubeconfigView copies the metadata only; file is the path written, if any.
func newKubeconfigView(k *verda.ManagedKubernetesKubeconfig, file string) KubeconfigView {
	groups := k.Groups
	if groups == nil {
		groups = []string{}
	}
	return KubeconfigView{
		ID:          k.ID,
		DisplayName: k.DisplayName,
		CommonName:  k.CommonName,
		Groups:      groups,
		Status:      k.Status,
		NotAfter:    k.NotAfter,
		File:        file,
	}
}

// ConditionView is one cluster condition.
type ConditionView struct {
	Type               string    `json:"type"                 yaml:"type"`
	Status             string    `json:"status"               yaml:"status"`
	Reason             string    `json:"reason"               yaml:"reason"`
	Message            string    `json:"message"              yaml:"message"`
	LastTransitionTime time.Time `json:"last_transition_time" yaml:"last_transition_time"`
}

// ClusterView is the JSON/YAML shape of a cluster; the keys follow the API.
type ClusterView struct {
	Name              string          `json:"name"                yaml:"name"`
	Location          string          `json:"location"            yaml:"location"`
	KubernetesVersion string          `json:"kubernetes_version"  yaml:"kubernetes_version"`
	Phase             string          `json:"phase"               yaml:"phase"`
	ExternalEndpoint  string          `json:"external_endpoint"   yaml:"external_endpoint"`
	NetworkID         string          `json:"network"             yaml:"network"`
	SubnetID          string          `json:"subnet"              yaml:"subnet"`
	PodCIDR           string          `json:"pod_cidr"            yaml:"pod_cidr"`
	ServiceCIDR       string          `json:"service_cidr"        yaml:"service_cidr"`
	DNSSuffix         string          `json:"dns_suffix"          yaml:"dns_suffix"`
	Conditions        []ConditionView `json:"conditions"          yaml:"conditions"`
	CreatedAt         time.Time       `json:"created_at"          yaml:"created_at"`
}

func newClusterView(c *verda.ManagedKubernetesCluster) ClusterView {
	conds := make([]ConditionView, 0, len(c.Conditions))
	for i := range c.Conditions {
		cond := &c.Conditions[i]
		conds = append(conds, ConditionView{
			Type:               cond.Type,
			Status:             cond.Status,
			Reason:             cond.Reason,
			Message:            cond.Message,
			LastTransitionTime: cond.LastTransitionTime,
		})
	}
	return ClusterView{
		Name:              c.Name,
		Location:          c.Location,
		KubernetesVersion: c.KubernetesVersion,
		Phase:             c.Phase,
		ExternalEndpoint:  c.ExternalEndpoint,
		NetworkID:         c.NetworkID,
		SubnetID:          c.SubnetID,
		PodCIDR:           c.PodCIDR,
		ServiceCIDR:       c.ServiceCIDR,
		DNSSuffix:         c.DNSSuffix,
		Conditions:        conds,
		CreatedAt:         c.CreatedAt,
	}
}

func newClusterViews(clusters []verda.ManagedKubernetesCluster) []ClusterView {
	views := make([]ClusterView, 0, len(clusters))
	for i := range clusters {
		views = append(views, newClusterView(&clusters[i]))
	}
	return views
}

// PoolSummaryView is a node pool as `cluster describe` lists it.
type PoolSummaryView struct {
	Name          string `json:"name"           yaml:"name"`
	InstanceType  string `json:"instance_type"  yaml:"instance_type"`
	ReadyReplicas int    `json:"ready_replicas" yaml:"ready_replicas"`
	Replicas      int    `json:"replicas"       yaml:"replicas"`
	State         string `json:"state"          yaml:"state"`
}

func newPoolSummaryViews(pools []verda.ManagedKubernetesNodePool) []PoolSummaryView {
	views := make([]PoolSummaryView, 0, len(pools))
	for i := range pools {
		p := &pools[i]
		views = append(views, PoolSummaryView{
			Name:          p.Name,
			InstanceType:  p.InstanceType,
			ReadyReplicas: p.ReadyReplicas,
			Replicas:      p.Replicas,
			State:         p.State,
		})
	}
	return views
}

// ClusterDescribeView is a cluster with its node pools. node_pools is null
// (and node_pools_error set) when the pool call failed, [] when there are none.
// It is a pointer so YAML, which writes a nil slice as [], also says null.
type ClusterDescribeView struct {
	ClusterView    `yaml:",inline"`
	NodePools      *[]PoolSummaryView `json:"node_pools"                 yaml:"node_pools"`
	NodePoolsError string             `json:"node_pools_error,omitempty" yaml:"node_pools_error,omitempty"`
}

// ageColumn renders the time since t the way kubectl does: 45s, 12m, 3h, 2d.
func ageColumn(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ActionResult is the structured output of a command that only accepts a
// request, such as a delete. Cluster is set for resources that belong to one.
type ActionResult struct {
	Resource string `json:"resource"          yaml:"resource"`
	ID       string `json:"id,omitempty"      yaml:"id,omitempty"`
	Name     string `json:"name"              yaml:"name"`
	Location string `json:"location"          yaml:"location"`
	Cluster  string `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	Action   string `json:"action"            yaml:"action"`
	Status   string `json:"status"            yaml:"status"`
}

// utcTime renders a time as "2006-01-02 15:04 UTC", the form of every date in mk8s output.
func utcTime(t time.Time) string {
	return t.UTC().Format(timeLayout) + " UTC"
}

// timeColumn renders a time for a table, "-" when it is not set.
func timeColumn(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return utcTime(t)
}

// PoolErrorView is one reason a node pool is not healthy.
type PoolErrorView struct {
	Reason  string    `json:"reason"  yaml:"reason"`
	Message string    `json:"message" yaml:"message"`
	Since   time.Time `json:"since"   yaml:"since"`
}

// PoolView is the JSON/YAML shape of a node pool; the keys follow the API.
type PoolView struct {
	Name          string          `json:"name"           yaml:"name"`
	InstanceType  string          `json:"instance_type"  yaml:"instance_type"`
	Replicas      int             `json:"replicas"       yaml:"replicas"`
	ReadyReplicas int             `json:"ready_replicas" yaml:"ready_replicas"`
	State         string          `json:"state"          yaml:"state"`
	SSHKeyIDs     []string        `json:"ssh_key_ids"    yaml:"ssh_key_ids"`
	Errors        []PoolErrorView `json:"errors"         yaml:"errors"`
	CreatedAt     time.Time       `json:"created_at"     yaml:"created_at"`
}

func newPoolView(p *verda.ManagedKubernetesNodePool) PoolView {
	errs := make([]PoolErrorView, 0, len(p.Errors))
	for i := range p.Errors {
		errs = append(errs, PoolErrorView{Reason: p.Errors[i].Reason, Message: p.Errors[i].Message, Since: p.Errors[i].Since})
	}
	keys := p.SSHKeyIDs
	if keys == nil {
		keys = []string{}
	}
	return PoolView{
		Name:          p.Name,
		InstanceType:  p.InstanceType,
		Replicas:      p.Replicas,
		ReadyReplicas: p.ReadyReplicas,
		State:         p.State,
		SSHKeyIDs:     keys,
		Errors:        errs,
		CreatedAt:     p.CreatedAt,
	}
}

func newPoolViews(pools []verda.ManagedKubernetesNodePool) []PoolView {
	views := make([]PoolView, 0, len(pools))
	for i := range pools {
		views = append(views, newPoolView(&pools[i]))
	}
	return views
}
