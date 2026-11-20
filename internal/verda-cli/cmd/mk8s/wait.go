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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// maxReadFailures is how many reads in a row may fail before a wait gives up.
// One 502 from a load balancer must not fail a 15-minute wait.
const maxReadFailures = 3

// pollInterval is the time between reads while --wait runs; a var so tests can shorten it.
var pollInterval = 5 * time.Second

var errWaitTimeout = errors.New("timed out waiting")

// pollReadError marks reads that kept failing, so the failure is not mistaken for a timeout.
type pollReadError struct{ err error }

func (e *pollReadError) Error() string { return e.err.Error() }
func (e *pollReadError) Unwrap() error { return e.err }

// fatalRead: a read that retrying cannot fix (bad request, no access). A 404
// is not fatal: right after a create the read can lag behind.
func fatalRead(err error) bool {
	apiErr, ok := errors.AsType[*verda.APIError](err)
	if !ok || apiErr.StatusCode < http.StatusBadRequest || apiErr.StatusCode >= http.StatusInternalServerError {
		return false
	}
	switch apiErr.StatusCode {
	case http.StatusNotFound, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return true
}

// waitFor polls get until check reports done. Each read has its own --timeout;
// the whole wait ends after timeout. It returns the last resource read and one
// of: nil (done), a *pollReadError (reads kept failing), or errWaitTimeout.
func waitFor[T any](
	ctx context.Context, f cmdutil.Factory, w io.Writer, timeout time.Duration,
	get func(context.Context) (*T, error), check func(*T) (status string, done bool),
) (*T, error) {
	var last *T
	failures := 0
	status := "waiting"
	pollFn := func(ctx context.Context) (string, bool, error) {
		readCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
		defer cancel()
		v, err := get(readCtx)
		if err != nil {
			// The whole wait ran out during this read: that is a timeout, not a failed read.
			if ctx.Err() != nil {
				return status, false, nil
			}
			// Until a first read works, a 404 is a read lagging behind the create: wait it out.
			if apiErr, ok := errors.AsType[*verda.APIError](err); ok && apiErr.StatusCode == http.StatusNotFound && last == nil {
				return status, false, nil
			}
			failures++
			if fatalRead(err) || failures >= maxReadFailures {
				return "", false, &pollReadError{err: mapErr(err)}
			}
			return status, false, nil
		}
		failures = 0
		last = v
		var done bool
		status, done = check(v)
		return status, done, nil
	}
	_, err := cmdutil.Poll(ctx, w, pollInterval, cmdutil.WaitOptions{Wait: true, Timeout: timeout}, pollFn)
	if readErr, ok := errors.AsType[*pollReadError](err); ok {
		return last, readErr
	}
	if err != nil {
		return last, errWaitTimeout
	}
	return last, nil
}

// waitWriter is where the wait's spinner line goes: stderr, and only for a
// human at a terminal, so stdout and structured output stay clean.
func waitWriter(f cmdutil.Factory, ioStreams cmdutil.IOStreams) io.Writer {
	if f.OutputFormat() == outputTable && !f.AgentMode() && cmdutil.IsStderrTerminal() {
		return ioStreams.ErrOut
	}
	return nil
}

// clusterReady: the cluster is usable once it is running and has an endpoint.
func clusterReady(c *verda.ManagedKubernetesCluster) (status string, done bool) {
	return c.Phase, c.Phase == verda.ManagedKubernetesClusterPhaseRunning && c.ExternalEndpoint != ""
}

// waitFailureInfo describes what a failed wait was waiting for.
type waitFailureInfo struct {
	Subject     string // for example "cluster prod in FIN-03"
	Timeout     time.Duration
	LastSeen    string // what the last read showed
	TimeoutTail string // what the user should know or do after a timeout
	ReadNote    string // optional: what the user should know when the reads kept failing
	CheckCmd    string
}

// waitFailure turns the error of waitFor into the message the command returns.
func waitFailure(err error, w *waitFailureInfo) error {
	if readErr, ok := errors.AsType[*pollReadError](err); ok {
		note := ""
		if w.ReadNote != "" {
			note = ". " + w.ReadNote
		}
		return fmt.Errorf("%s was accepted, but --wait could not read it (%w); check with `%s`%s", w.Subject, readErr.err, w.CheckCmd, note)
	}
	return fmt.Errorf("timed out after %s waiting for %s (%s). %s", w.Timeout, w.Subject, w.LastSeen, w.TimeoutTail)
}

// poolReasons lists the distinct error reasons of a pool, in order.
func poolReasons(p *verda.ManagedKubernetesNodePool) []string {
	var reasons []string
	for i := range p.Errors {
		if !slices.Contains(reasons, p.Errors[i].Reason) {
			reasons = append(reasons, p.Errors[i].Reason)
		}
	}
	return reasons
}

// poolStatus is the one-line state shown while waiting: the state, the error
// reasons while the pool is degraded, and ready/desired workers.
func poolStatus(p *verda.ManagedKubernetesNodePool) string {
	s := p.State
	if reasons := poolReasons(p); p.State == verda.ManagedKubernetesNodePoolStateDegraded && len(reasons) > 0 {
		s += " (" + strings.Join(reasons, ", ") + ")"
	}
	return fmt.Sprintf("%s, %d/%d workers ready", s, p.ReadyReplicas, p.Replicas)
}

// poolCreated is done when the pool is active with the requested number of
// ready workers. Comparing with the requested count, not the count read, keeps
// a stale first read from ending the wait.
func poolCreated(want int) func(*verda.ManagedKubernetesNodePool) (string, bool) {
	return func(p *verda.ManagedKubernetesNodePool) (string, bool) {
		return poolStatus(p), p.State == verda.ManagedKubernetesNodePoolStateActive && p.ReadyReplicas == want
	}
}

// poolScaled is done when the API reports the new count, all of it ready, and the pool active.
func poolScaled(want int) func(*verda.ManagedKubernetesNodePool) (string, bool) {
	return func(p *verda.ManagedKubernetesNodePool) (string, bool) {
		done := p.State == verda.ManagedKubernetesNodePoolStateActive && p.Replicas == want && p.ReadyReplicas == want
		return poolStatus(p), done
	}
}

// writeResult writes a structured result and returns waitErr, so a failed wait
// still leaves the result on stdout. It reports false for table output.
func writeResult(f cmdutil.Factory, ioStreams cmdutil.IOStreams, result any, waitErr error) (written bool, err error) {
	wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
	if !wrote {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, waitErr
}

// waitForPool polls the pool until done. It returns the last pool read.
func waitForPool(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, t *clusterTarget, name string, done func(*verda.ManagedKubernetesNodePool) (string, bool), timeout time.Duration) (*verda.ManagedKubernetesNodePool, error) {
	if f.OutputFormat() == outputTable {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Node pool %s accepted; waiting for its workers (Ctrl+C stops waiting, the pool stays).\n", name)
	}
	return waitFor(ctx, f, waitWriter(f, ioStreams), timeout,
		func(ctx context.Context) (*verda.ManagedKubernetesNodePool, error) {
			return client.ManagedKubernetes.GetNodePool(ctx, t.Location, t.Name, name)
		}, done)
}

// poolWaitKind says which change a pool wait was for: its timeout text differs.
type poolWaitKind int

const (
	poolWaitCreate poolWaitKind = iota
	poolWaitScaleUp
	poolWaitScaleDown
	poolWaitSettle
)

// poolWaitFailure is the error of a pool wait that did not finish. When workers
// are being created, a timeout also says they keep billing and how to stop
// them: delete the pool after a create, scale back to previous after a scale-up.
func poolWaitFailure(err error, kind poolWaitKind, t *clusterTarget, accepted, last *verda.ManagedKubernetesNodePool, timeout time.Duration, previous int) error {
	seen := accepted
	if last != nil {
		seen = last
	}
	describe := poolDescribeCmd(t, accepted.Name)
	subject := fmt.Sprintf("node pool %s in cluster %s (%s)", accepted.Name, t.Name, t.Location)
	creating := poolBackendKeepsTrying + " To stop: `" + poolDeleteCmd(t, accepted.Name) + "`. Check with `" + describe + "`"
	info := waitFailureInfo{Subject: subject, Timeout: timeout, LastSeen: poolSeen(seen), CheckCmd: describe}
	switch kind {
	case poolWaitScaleDown:
		info.Subject = "scale-down of " + subject
		info.TimeoutTail = "The API is still removing workers. Check with `" + describe + "`"
	case poolWaitSettle:
		info.Subject = "scaling of " + subject
		info.TimeoutTail = poolBackendKeepsTrying + " Check with `" + describe + "`"
		info.ReadNote = poolBackendKeepsTrying
	case poolWaitScaleUp:
		info.Subject = "scaling of " + subject
		info.TimeoutTail = poolBackendKeepsTrying + " To stop, scale back: `" + poolScaleCmd(t, accepted.Name, previous) + " --yes`. Check with `" + describe + "`"
		info.ReadNote = poolBackendKeepsTrying
	default:
		info.TimeoutTail, info.ReadNote = creating, poolBackendKeepsTrying
	}
	return waitFailure(err, &info)
}

// poolSeen is what a timeout message says about the last read: state, ready/desired, error reasons.
func poolSeen(p *verda.ManagedKubernetesNodePool) string {
	s := fmt.Sprintf("last state: %s, %d/%d workers ready", p.State, p.ReadyReplicas, p.Replicas)
	if reasons := poolReasons(p); len(reasons) > 0 {
		s += ", errors: " + strings.Join(reasons, ", ")
	}
	return s
}

// kubeconfigReady: the YAML exists only once the kubeconfig is ready. Both are
// checked, so a read that says ready without the YAML does not end the wait.
func kubeconfigReady(k *verda.ManagedKubernetesKubeconfig) (status string, done bool) {
	return k.Status, k.Status == verda.ManagedKubernetesKubeconfigStatusReady && k.Kubeconfig != ""
}
