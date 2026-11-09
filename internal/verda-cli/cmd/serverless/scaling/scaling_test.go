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

package scaling

import (
	"errors"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestFieldError(t *testing.T) {
	t.Parallel()
	e := &FieldError{Field: "min-replicas", Message: "must be >= 0"}
	if got := e.Error(); got != "min-replicas: must be >= 0" {
		t.Errorf("Error() = %q", got)
	}
}

func TestChange_Empty(t *testing.T) {
	t.Parallel()
	if !(&Change{}).Empty() {
		t.Error("zero Change should be empty")
	}
	if (&Change{MinReplicas: new(0)}).Empty() {
		t.Error("Change with MinReplicas set should not be empty")
	}
	if (&Change{QueueLoad: new(float64(3))}).Empty() {
		t.Error("Change with QueueLoad set should not be empty")
	}
}

func TestChange_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		change  Change
		wantErr string
	}{
		{"empty is valid", Change{}, ""},
		{"min replicas 0", Change{MinReplicas: new(0)}, ""},
		{"min replicas max", Change{MinReplicas: new(MaxReplicas)}, ""},
		{"min replicas negative", Change{MinReplicas: new(-1)}, "min-replicas"},
		{"min replicas over max", Change{MinReplicas: new(MaxReplicas + 1)}, "min-replicas"},
		{"max replicas 1", Change{MaxReplicas: new(1)}, ""},
		{"max replicas 0", Change{MaxReplicas: new(0)}, "max-replicas"},
		{"max replicas over", Change{MaxReplicas: new(MaxReplicas + 1)}, "max-replicas"},
		{"concurrency 1", Change{Concurrency: new(1)}, ""},
		{"concurrency 0", Change{Concurrency: new(0)}, "concurrency"},
		{"concurrency over", Change{Concurrency: new(MaxConcurrency + 1)}, "concurrency"},
		{"request-ttl 0", Change{QueueTTLSeconds: new(0)}, ""},
		{"request-ttl max", Change{QueueTTLSeconds: new(MaxQueueTTLSeconds)}, ""},
		{"request-ttl over", Change{QueueTTLSeconds: new(MaxQueueTTLSeconds + 1)}, "request-ttl"},
		{"scale-up-delay max", Change{ScaleUpDelaySeconds: new(MaxDelaySeconds)}, ""},
		{"scale-up-delay over", Change{ScaleUpDelaySeconds: new(MaxDelaySeconds + 1)}, "scale-up-delay"},
		{"scale-down-delay over", Change{ScaleDownDelaySeconds: new(MaxDelaySeconds + 1)}, "scale-down-delay"},
		{"cpu-util valid", Change{CPUUtil: new(50)}, ""},
		{"cpu-util off", Change{CPUUtil: new(0)}, ""},
		{"cpu-util below min", Change{CPUUtil: new(MinUtilThreshold - 1)}, "cpu-util"},
		{"cpu-util above max", Change{CPUUtil: new(MaxUtilThreshold + 1)}, "cpu-util"},
		{"gpu-util valid", Change{GPUUtil: new(80)}, ""},
		{"gpu-util off", Change{GPUUtil: new(0)}, ""},
		{"gpu-util below min", Change{GPUUtil: new(MinUtilThreshold - 1)}, "gpu-util"},
		{"queue-load valid", Change{QueueLoad: new(float64(5))}, ""},
		{"queue-load below min", Change{QueueLoad: new(float64(0))}, "queue-load"},
		{"queue-load above max", Change{QueueLoad: new(float64(MaxQueueLoad + 1))}, "queue-load"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.change.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			var fe *FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("error type = %T, want *FieldError", err)
			}
			if fe.Field != tt.wantErr {
				t.Errorf("FieldError.Field = %q, want %q", fe.Field, tt.wantErr)
			}
		})
	}
}

func TestChange_Request_BasicFields(t *testing.T) {
	t.Parallel()
	c := &Change{
		MinReplicas:           new(0),
		MaxReplicas:           new(10),
		Concurrency:           new(4),
		QueueTTLSeconds:       new(600),
		ScaleUpDelaySeconds:   new(30),
		ScaleDownDelaySeconds: new(60),
	}
	req, err := c.Request()
	if err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	if req.MinReplicaCount == nil || *req.MinReplicaCount != 0 {
		t.Errorf("MinReplicaCount = %v, want 0", req.MinReplicaCount)
	}
	if req.MaxReplicaCount == nil || *req.MaxReplicaCount != 10 {
		t.Errorf("MaxReplicaCount = %v, want 10", req.MaxReplicaCount)
	}
	if req.ConcurrentRequestsPerReplica == nil || *req.ConcurrentRequestsPerReplica != 4 {
		t.Errorf("Concurrency = %v, want 4", req.ConcurrentRequestsPerReplica)
	}
	if req.QueueMessageTTLSeconds == nil || *req.QueueMessageTTLSeconds != 600 {
		t.Errorf("QueueMessageTTLSeconds = %v, want 600", req.QueueMessageTTLSeconds)
	}
	if req.ScaleUpPolicy == nil || req.ScaleUpPolicy.DelaySeconds != 30 {
		t.Errorf("ScaleUpPolicy = %+v, want 30s", req.ScaleUpPolicy)
	}
	if req.ScaleDownPolicy == nil || req.ScaleDownPolicy.DelaySeconds != 60 {
		t.Errorf("ScaleDownPolicy = %+v, want 60s", req.ScaleDownPolicy)
	}
}

func TestChange_Request_Triggers(t *testing.T) {
	t.Parallel()
	c := &Change{
		MaxReplicas: new(5),
		QueueLoad:   new(float64(3)),
		CPUUtil:     new(70),
		GPUUtil:     new(0),
	}
	req, err := c.Request()
	if err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	if req.ScalingTriggers == nil {
		t.Fatal("ScalingTriggers should be set")
	}
	if req.ScalingTriggers.QueueLoad == nil || req.ScalingTriggers.QueueLoad.Threshold != 3 {
		t.Errorf("QueueLoad = %+v, want threshold=3", req.ScalingTriggers.QueueLoad)
	}
	if req.ScalingTriggers.CPUUtilization == nil || !req.ScalingTriggers.CPUUtilization.Enabled || req.ScalingTriggers.CPUUtilization.Threshold != 70 {
		t.Errorf("CPUUtilization = %+v, want enabled@70", req.ScalingTriggers.CPUUtilization)
	}
	if req.ScalingTriggers.GPUUtilization == nil || req.ScalingTriggers.GPUUtilization.Enabled {
		t.Errorf("GPUUtilization = %+v, want disabled", req.ScalingTriggers.GPUUtilization)
	}
}

func TestChange_Request_NoTriggers(t *testing.T) {
	t.Parallel()
	c := &Change{MaxReplicas: new(5)}
	req, err := c.Request()
	if err != nil {
		t.Fatalf("Request() error: %v", err)
	}
	if req.ScalingTriggers != nil {
		t.Errorf("ScalingTriggers should be nil when no trigger fields set")
	}
}

func TestChange_Request_MinGreaterThanMax(t *testing.T) {
	t.Parallel()
	c := &Change{MinReplicas: new(5), MaxReplicas: new(3)}
	_, err := c.Request()
	if err == nil {
		t.Fatal("Request() should fail when min > max")
	}
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("error type = %T, want *FieldError", err)
	}
	if fe.Field != "min-replicas" {
		t.Errorf("FieldError.Field = %q, want min-replicas", fe.Field)
	}
}

func TestChange_Request_ValidationError(t *testing.T) {
	t.Parallel()
	c := &Change{MaxReplicas: new(0)}
	_, err := c.Request()
	if err == nil {
		t.Fatal("Request() should fail for max-replicas=0")
	}
}

func TestTriggers_Nil(t *testing.T) {
	t.Parallel()
	sc := &verda.ContainerScalingOptions{}
	got := Triggers(sc)
	if got.QueueLoad != nil || got.CPUUtilization != nil || got.GPUUtilization != nil {
		t.Errorf("Triggers with nil ScalingTriggers should be zero; got %+v", got)
	}
}

func TestTriggers_NonNil(t *testing.T) {
	t.Parallel()
	sc := &verda.ContainerScalingOptions{
		ScalingTriggers: &verda.ScalingTriggers{
			QueueLoad: &verda.QueueLoadTrigger{Threshold: 3},
		},
	}
	got := Triggers(sc)
	if got.QueueLoad == nil || got.QueueLoad.Threshold != 3 {
		t.Errorf("Triggers should pass through; got %+v", got)
	}
}

func TestBoundWarning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cur  *verda.ContainerScalingOptions
		req  *verda.UpdateScalingOptionsRequest
		want bool
	}{
		{
			"no warning when no bounds",
			&verda.ContainerScalingOptions{MinReplicaCount: 1, MaxReplicaCount: 10},
			&verda.UpdateScalingOptionsRequest{},
			false,
		},
		{
			"min above max",
			&verda.ContainerScalingOptions{MinReplicaCount: 1, MaxReplicaCount: 3},
			&verda.UpdateScalingOptionsRequest{MinReplicaCount: new(5)},
			true,
		},
		{
			"max below min",
			&verda.ContainerScalingOptions{MinReplicaCount: 5, MaxReplicaCount: 10},
			&verda.UpdateScalingOptionsRequest{MaxReplicaCount: new(3)},
			true,
		},
		{
			"both bounds set - no warning (validated by Request)",
			&verda.ContainerScalingOptions{MinReplicaCount: 5, MaxReplicaCount: 10},
			&verda.UpdateScalingOptionsRequest{MinReplicaCount: new(1), MaxReplicaCount: new(10)},
			false,
		},
		{
			"min equal to max - no warning",
			&verda.ContainerScalingOptions{MinReplicaCount: 1, MaxReplicaCount: 5},
			&verda.UpdateScalingOptionsRequest{MinReplicaCount: new(5)},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := BoundWarning(tt.cur, tt.req)
			if tt.want && w == "" {
				t.Error("expected warning, got empty")
			}
			if !tt.want && w != "" {
				t.Errorf("expected no warning, got %q", w)
			}
		})
	}
}

func TestApplied(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  *verda.ContainerScalingOptions
		req  *verda.UpdateScalingOptionsRequest
		want bool
	}{
		{
			"empty request always matches",
			&verda.ContainerScalingOptions{MinReplicaCount: 1, MaxReplicaCount: 3},
			&verda.UpdateScalingOptionsRequest{},
			true,
		},
		{
			"min replicas match",
			&verda.ContainerScalingOptions{MinReplicaCount: 2},
			&verda.UpdateScalingOptionsRequest{MinReplicaCount: new(2)},
			true,
		},
		{
			"min replicas mismatch",
			&verda.ContainerScalingOptions{MinReplicaCount: 1},
			&verda.UpdateScalingOptionsRequest{MinReplicaCount: new(2)},
			false,
		},
		{
			"max replicas match",
			&verda.ContainerScalingOptions{MaxReplicaCount: 10},
			&verda.UpdateScalingOptionsRequest{MaxReplicaCount: new(10)},
			true,
		},
		{
			"concurrency match",
			&verda.ContainerScalingOptions{ConcurrentRequestsPerReplica: 4},
			&verda.UpdateScalingOptionsRequest{ConcurrentRequestsPerReplica: new(4)},
			true,
		},
		{
			"policy match",
			&verda.ContainerScalingOptions{
				ScaleUpPolicy: &verda.ScalingPolicy{DelaySeconds: 30},
			},
			&verda.UpdateScalingOptionsRequest{
				ScaleUpPolicy: &verda.ScalingPolicy{DelaySeconds: 30},
			},
			true,
		},
		{
			"policy mismatch",
			&verda.ContainerScalingOptions{
				ScaleUpPolicy: &verda.ScalingPolicy{DelaySeconds: 30},
			},
			&verda.UpdateScalingOptionsRequest{
				ScaleUpPolicy: &verda.ScalingPolicy{DelaySeconds: 60},
			},
			false,
		},
		{
			"policy 0s want nil got",
			&verda.ContainerScalingOptions{},
			&verda.UpdateScalingOptionsRequest{
				ScaleUpPolicy: &verda.ScalingPolicy{DelaySeconds: 0},
			},
			true,
		},
		{
			"queue load trigger match",
			&verda.ContainerScalingOptions{
				ScalingTriggers: &verda.ScalingTriggers{
					QueueLoad: &verda.QueueLoadTrigger{Threshold: 3},
				},
			},
			&verda.UpdateScalingOptionsRequest{
				ScalingTriggers: &verda.ScalingTriggers{
					QueueLoad: &verda.QueueLoadTrigger{Threshold: 3},
				},
			},
			true,
		},
		{
			"queue load trigger mismatch",
			&verda.ContainerScalingOptions{
				ScalingTriggers: &verda.ScalingTriggers{
					QueueLoad: &verda.QueueLoadTrigger{Threshold: 3},
				},
			},
			&verda.UpdateScalingOptionsRequest{
				ScalingTriggers: &verda.ScalingTriggers{
					QueueLoad: &verda.QueueLoadTrigger{Threshold: 6},
				},
			},
			false,
		},
		{
			"cpu util enabled match",
			&verda.ContainerScalingOptions{
				ScalingTriggers: &verda.ScalingTriggers{
					CPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 70},
				},
			},
			&verda.UpdateScalingOptionsRequest{
				ScalingTriggers: &verda.ScalingTriggers{
					CPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 70},
				},
			},
			true,
		},
		{
			"cpu util disabled match (nil got)",
			&verda.ContainerScalingOptions{},
			&verda.UpdateScalingOptionsRequest{
				ScalingTriggers: &verda.ScalingTriggers{
					CPUUtilization: &verda.UtilizationTrigger{},
				},
			},
			true,
		},
		{
			"cpu util disabled match (got disabled)",
			&verda.ContainerScalingOptions{
				ScalingTriggers: &verda.ScalingTriggers{
					CPUUtilization: &verda.UtilizationTrigger{Enabled: false},
				},
			},
			&verda.UpdateScalingOptionsRequest{
				ScalingTriggers: &verda.ScalingTriggers{
					CPUUtilization: &verda.UtilizationTrigger{},
				},
			},
			true,
		},
		{
			"gpu util enabled mismatch",
			&verda.ContainerScalingOptions{
				ScalingTriggers: &verda.ScalingTriggers{
					GPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 50},
				},
			},
			&verda.UpdateScalingOptionsRequest{
				ScalingTriggers: &verda.ScalingTriggers{
					GPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 80},
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Applied(tt.got, tt.req); got != tt.want {
				t.Errorf("Applied() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUtilTrigger(t *testing.T) {
	t.Parallel()
	if utilTrigger(nil) != nil {
		t.Error("nil input should return nil")
	}
	zero := 0
	got := utilTrigger(&zero)
	if got == nil || got.Enabled || got.Threshold != 0 {
		t.Errorf("utilTrigger(0) = %+v, want disabled", got)
	}
	seventy := 70
	got = utilTrigger(&seventy)
	if got == nil || !got.Enabled || got.Threshold != 70 {
		t.Errorf("utilTrigger(70) = %+v, want enabled@70", got)
	}
}

func TestPermanent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"non-api error", errors.New("network"), false},
		{"400", &verda.APIError{StatusCode: 400}, true},
		{"404", &verda.APIError{StatusCode: 404}, true},
		{"408 (not permanent)", &verda.APIError{StatusCode: 408}, false},
		{"429 (not permanent)", &verda.APIError{StatusCode: 429}, false},
		{"500 (not permanent)", &verda.APIError{StatusCode: 500}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := permanent(tt.err); got != tt.want {
				t.Errorf("permanent() = %v, want %v", got, tt.want)
			}
		})
	}
}
