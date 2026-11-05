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

// Package scaling holds the scaling rules for serverless container
// deployments: limits, partial-update request building, and the check that
// reads show an update.
package scaling

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// Limits from PatchScalingOptionsPublicApiDto. The queue-load range is not in
// the spec; the SDK enforces it.
const (
	MaxReplicas        = 1000
	MaxConcurrency     = 5000
	MinUtilThreshold   = 5
	MaxUtilThreshold   = 100
	MinQueueLoad       = 1
	MaxQueueLoad       = 1000
	MaxDelaySeconds    = 3600
	MaxQueueTTLSeconds = 86400
)

// PollInterval: scaling reads lag writes by about 5-20 s.
const PollInterval = 2 * time.Second

// FieldError reports an invalid value. Field is the option name in CLI flag
// form without dashes ("min-replicas"); callers render it for their surface.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Change is a partial scaling update; nil fields stay unchanged.
// CPUUtil/GPUUtil: a threshold turns the trigger on, 0 turns it off.
type Change struct {
	MinReplicas           *int
	MaxReplicas           *int
	Concurrency           *int
	QueueTTLSeconds       *int
	ScaleUpDelaySeconds   *int
	ScaleDownDelaySeconds *int
	QueueLoad             *float64
	CPUUtil               *int
	GPUUtil               *int
}

// Triggers returns sc's triggers, or the zero value when the read has none.
func Triggers(sc *verda.ContainerScalingOptions) verda.ScalingTriggers {
	if sc.ScalingTriggers == nil {
		return verda.ScalingTriggers{}
	}
	return *sc.ScalingTriggers
}

// Empty reports whether no option is set.
func (c *Change) Empty() bool {
	return c.MinReplicas == nil && c.MaxReplicas == nil && c.Concurrency == nil && c.QueueTTLSeconds == nil &&
		c.ScaleUpDelaySeconds == nil && c.ScaleDownDelaySeconds == nil && c.QueueLoad == nil && c.CPUUtil == nil && c.GPUUtil == nil
}

// Validate checks set values against the spec, which is stricter than the SDK
// (it rejects max 0, concurrency 0, enabled threshold 0).
func (c *Change) Validate() error {
	ints := []struct {
		field  string
		v      *int
		lo, hi int
		hint   string
	}{
		{"min-replicas", c.MinReplicas, 0, MaxReplicas, ""},
		{"max-replicas", c.MaxReplicas, 1, MaxReplicas, "; for no replicas while idle, set the minimum to 0 or pause the deployment"},
		{"concurrency", c.Concurrency, 1, MaxConcurrency, ""},
		{"request-ttl", c.QueueTTLSeconds, 0, MaxQueueTTLSeconds, " seconds"},
		{"scale-up-delay", c.ScaleUpDelaySeconds, 0, MaxDelaySeconds, " seconds"},
		{"scale-down-delay", c.ScaleDownDelaySeconds, 0, MaxDelaySeconds, " seconds"},
	}
	for _, f := range ints {
		if f.v != nil && (*f.v < f.lo || *f.v > f.hi) {
			return &FieldError{f.field, fmt.Sprintf("must be between %d and %d%s", f.lo, f.hi, f.hint)}
		}
	}
	for _, u := range []struct {
		field string
		v     *int
	}{{"cpu-util", c.CPUUtil}, {"gpu-util", c.GPUUtil}} {
		if u.v != nil && *u.v != 0 && (*u.v < MinUtilThreshold || *u.v > MaxUtilThreshold) {
			return &FieldError{u.field, fmt.Sprintf("must be %d..%d or off", MinUtilThreshold, MaxUtilThreshold)}
		}
	}
	if q := c.QueueLoad; q != nil && (*q < MinQueueLoad || *q > MaxQueueLoad) {
		return &FieldError{"queue-load", fmt.Sprintf("must be between %d and %d", MinQueueLoad, MaxQueueLoad)}
	}
	return nil
}

// Request builds the PATCH body; it never copies from a read, which can lag and undo a recent write.
func (c *Change) Request() (*verda.UpdateScalingOptionsRequest, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.MinReplicas != nil && c.MaxReplicas != nil && *c.MinReplicas > *c.MaxReplicas {
		return nil, &FieldError{"min-replicas", fmt.Sprintf("minimum replicas (%d) must not be greater than maximum replicas (%d)", *c.MinReplicas, *c.MaxReplicas)}
	}

	req := verda.UpdateScalingOptionsRequest{
		MinReplicaCount:              c.MinReplicas,
		MaxReplicaCount:              c.MaxReplicas,
		QueueMessageTTLSeconds:       c.QueueTTLSeconds,
		ConcurrentRequestsPerReplica: c.Concurrency,
	}
	if c.ScaleUpDelaySeconds != nil {
		req.ScaleUpPolicy = &verda.ScalingPolicy{DelaySeconds: *c.ScaleUpDelaySeconds}
	}
	if c.ScaleDownDelaySeconds != nil {
		req.ScaleDownPolicy = &verda.ScalingPolicy{DelaySeconds: *c.ScaleDownDelaySeconds}
	}
	// Omitted triggers keep their values server-side.
	cpu, gpu := utilTrigger(c.CPUUtil), utilTrigger(c.GPUUtil)
	if c.QueueLoad != nil || cpu != nil || gpu != nil {
		req.ScalingTriggers = &verda.ScalingTriggers{CPUUtilization: cpu, GPUUtilization: gpu}
		if c.QueueLoad != nil {
			req.ScalingTriggers.QueueLoad = &verda.QueueLoadTrigger{Threshold: *c.QueueLoad}
		}
	}
	return &req, nil
}

// BoundWarning: req's one bound against the other in cur (which can lag) → a warning; else "".
func BoundWarning(cur *verda.ContainerScalingOptions, req *verda.UpdateScalingOptionsRequest) string {
	const lag = " in the last read, which can lag a recent change; the API rejects it if the stored "
	switch {
	case req.MinReplicaCount != nil && req.MaxReplicaCount == nil && *req.MinReplicaCount > cur.MaxReplicaCount:
		return fmt.Sprintf("min replicas %d is above max replicas %d%smax is lower", *req.MinReplicaCount, cur.MaxReplicaCount, lag)
	case req.MaxReplicaCount != nil && req.MinReplicaCount == nil && *req.MaxReplicaCount < cur.MinReplicaCount:
		return fmt.Sprintf("max replicas %d is below min replicas %d%smin is higher", *req.MaxReplicaCount, cur.MinReplicaCount, lag)
	}
	return ""
}

// utilTrigger: nil leaves the trigger out, 0 disables it (always sent: reads
// lag, so "already off" may be stale), n enables it at n.
func utilTrigger(t *int) *verda.UtilizationTrigger {
	switch {
	case t == nil:
		return nil
	case *t == 0:
		return &verda.UtilizationTrigger{}
	default:
		return &verda.UtilizationTrigger{Enabled: true, Threshold: *t}
	}
}

// Applied reports whether got shows every field req sets. Fields req
// leaves out are not compared: the pre-read they would come from may itself
// lag an earlier update.
func Applied(got *verda.ContainerScalingOptions, req *verda.UpdateScalingOptionsRequest) bool {
	ints := []struct {
		want *int
		got  int
	}{
		{req.MinReplicaCount, got.MinReplicaCount},
		{req.MaxReplicaCount, got.MaxReplicaCount},
		{req.QueueMessageTTLSeconds, got.QueueMessageTTLSeconds},
		{req.ConcurrentRequestsPerReplica, got.ConcurrentRequestsPerReplica},
	}
	for _, f := range ints {
		if f.want != nil && *f.want != f.got {
			return false
		}
	}
	if !policyApplied(req.ScaleUpPolicy, got.ScaleUpPolicy) || !policyApplied(req.ScaleDownPolicy, got.ScaleDownPolicy) {
		return false
	}
	t := req.ScalingTriggers
	if t == nil {
		return true
	}
	gt := Triggers(got)
	if t.QueueLoad != nil && !queueLoadApplied(t.QueueLoad, gt.QueueLoad) {
		return false
	}
	return utilApplied(t.CPUUtilization, gt.CPUUtilization) && utilApplied(t.GPUUtilization, gt.GPUUtilization)
}

// policyApplied: a read may return a 0 s policy as null.
func policyApplied(want, got *verda.ScalingPolicy) bool {
	if want == nil {
		return true
	}
	if got == nil {
		return want.DelaySeconds == 0
	}
	return *got == *want
}

// queueLoadApplied tolerates float noise in a stored fractional threshold.
func queueLoadApplied(want, got *verda.QueueLoadTrigger) bool {
	return got != nil && math.Abs(got.Threshold-want.Threshold) < 1e-6
}

// permanent: a 4xx other than 408/429 will not change by waiting.
func permanent(err error) bool {
	var apiErr *verda.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	s := apiErr.StatusCode
	return s >= 400 && s < 500 && s != http.StatusRequestTimeout && s != http.StatusTooManyRequests
}

// utilApplied: a disabled trigger reads back with threshold 0, so only enabled is compared.
func utilApplied(want, got *verda.UtilizationTrigger) bool {
	switch {
	case want == nil:
		return true
	case !want.Enabled:
		return got == nil || !got.Enabled
	default:
		return got != nil && *got == *want
	}
}

// Wait polls until reads show every field req sets (status on w, nil = silent); a 4xx read ends it at once.
func Wait(ctx context.Context, w io.Writer, client *verda.Client, name string, req *verda.UpdateScalingOptionsRequest, timeout time.Duration) (*verda.ContainerScalingOptions, error) {
	var got *verda.ContainerScalingOptions
	_, err := cmdutil.Poll(ctx, w, PollInterval, cmdutil.WaitOptions{Wait: true, Timeout: timeout},
		func(ctx context.Context) (string, bool, error) {
			sc, err := client.ContainerDeployments.GetDeploymentScaling(ctx, name)
			if permanent(err) {
				return "", false, err
			}
			if err != nil || !Applied(sc, req) {
				return "scaling change not visible yet", false, nil //nolint:nilerr // intentional: a transient read failure is "not yet"; the wait deadline bounds retries
			}
			got = sc
			return "applied", true, nil
		})
	if err != nil {
		return nil, err
	}
	return got, nil
}
