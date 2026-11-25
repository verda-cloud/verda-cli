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

package serverless

import (
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// The exported constructors are the MCP server's source of defaults; a drift
// here changes what agents deploy without touching a flag.
func TestNewContainerCreateOptionsDefaults(t *testing.T) {
	o := NewContainerCreateOptions()
	o.Name, o.Image, o.Compute = "d", "img:v1", "H100"
	req, err := o.Request()
	if err != nil {
		t.Fatalf("defaults must build a valid request: %v", err)
	}
	if req.Compute.Size != 1 || req.Containers[0].ExposedPort != defaultExposedPort {
		t.Errorf("compute size / port = %d / %d", req.Compute.Size, req.Containers[0].ExposedPort)
	}
	sc := req.Scaling
	if sc.MinReplicaCount != 0 || sc.MaxReplicaCount != defaultMaxReplicas || sc.ConcurrentRequestsPerReplica != defaultConcurrency {
		t.Errorf("replica defaults = %+v", sc)
	}
	if sc.ScalingTriggers.QueueLoad.Threshold != queueLoadBalanced {
		t.Errorf("default preset must be balanced (%d): %v", queueLoadBalanced, sc.ScalingTriggers.QueueLoad.Threshold)
	}
	if sc.ScaleDownPolicy.DelaySeconds != int(defaultScaleDownDelay.Seconds()) || sc.QueueMessageTTLSeconds != int(defaultRequestTTL.Seconds()) {
		t.Errorf("delay / ttl defaults = %d / %d", sc.ScaleDownPolicy.DelaySeconds, sc.QueueMessageTTLSeconds)
	}
	if hc := req.Containers[0].Healthcheck; hc == nil || hc.Path != defaultHealthcheckPath || hc.Port != defaultExposedPort {
		t.Errorf("healthcheck defaults = %+v", hc)
	}
}

func TestNewBatchjobCreateOptionsDefaults(t *testing.T) {
	o := NewBatchjobCreateOptions()
	o.Name, o.Image, o.Compute, o.Deadline = "j", "img:v1", "H100", 30*time.Minute
	req, err := o.Request()
	if err != nil {
		t.Fatalf("defaults plus a deadline must build a valid request: %v", err)
	}
	if req.Compute.Size != 1 || req.Containers[0].ExposedPort != defaultExposedPort {
		t.Errorf("compute size / port = %d / %d", req.Compute.Size, req.Containers[0].ExposedPort)
	}
	if req.Scaling.MaxReplicaCount != defaultMaxReplicas || req.Scaling.QueueMessageTTLSeconds != int(defaultRequestTTL.Seconds()) {
		t.Errorf("scaling defaults = %+v", req.Scaling)
	}
	if req.ContainerRegistrySettings != nil {
		t.Error("a public image must omit registry settings")
	}
}

func TestPartitionEnvVars(t *testing.T) {
	current := []verda.ContainerEnvVar{{Name: "A"}, {Name: "B"}}
	vars := []verda.ContainerEnvVar{{Name: "B", ValueOrReferenceToSecret: "2"}, {Name: "C"}, {Name: "A"}}
	add, upd := PartitionEnvVars(current, vars)
	if len(add) != 1 || add[0].Name != "C" {
		t.Errorf("toAdd = %v, want [C]", add)
	}
	if len(upd) != 2 || upd[0].Name != "B" || upd[1].Name != "A" {
		t.Errorf("toUpdate = %v, want [B A] in input order", upd)
	}
	if add, upd := PartitionEnvVars(nil, nil); add != nil || upd != nil {
		t.Errorf("empty input yields nil slices: %v %v", add, upd)
	}
}

func TestBuildEnvVarsRejectsBadSecretEntry(t *testing.T) {
	if _, err := BuildEnvVars(nil, []string{"novalue"}); err == nil {
		t.Fatal("a secret entry without '=' must be rejected")
	}
	if _, err := BuildEnvVars([]string{"A=1"}, []string{"lower=s"}); err == nil {
		t.Fatal("a lowercase secret name must be rejected")
	}
	out, err := BuildEnvVars([]string{"A=1"}, []string{"B=s"})
	if err != nil || len(out) != 2 || out[0].Type != envTypePlain || out[1].Type != envTypeSecret {
		t.Fatalf("mixed input: %v, %v", out, err)
	}
}

func TestEnvVarsOfMissingContainer(t *testing.T) {
	groups := []EnvVarGroup{{ContainerName: "d-1", Env: []verda.ContainerEnvVar{{Name: "X"}}}}
	if got := EnvVarsOf(groups, "d-0"); got != nil {
		t.Fatalf("a container absent from the groups has no vars, got %v", got)
	}
	if got := EnvVarsOf(groups, "d-1"); len(got) != 1 {
		t.Fatalf("own group: %v", got)
	}
}

func TestDeleteTimeoutBounds(t *testing.T) {
	cases := []struct {
		explicit bool
		ms       int
		want     time.Duration
	}{
		{true, -1, 7 * time.Second},                        // explicit --timeout wins
		{false, -1, defaultDeleteWait + deleteWaitMargin},  // API default wait
		{false, 0, deleteWaitMargin},                       // no server wait
		{false, 2000, 2*time.Second + deleteWaitMargin},    // explicit wait
		{false, 999_999, maxDeleteWait + deleteWaitMargin}, // capped like the server
	}
	for _, tc := range cases {
		if got := DeleteTimeout(7*time.Second, tc.explicit, tc.ms); got != tc.want {
			t.Errorf("DeleteTimeout(explicit=%v, ms=%d) = %s, want %s", tc.explicit, tc.ms, got, tc.want)
		}
	}
}
