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
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// maxReadFailures failed reads in a row end the wait; one 502 from a load balancer must not fail a 15-minute wait.
const maxReadFailures = 3

// pollInterval is a var so tests can shorten it.
var pollInterval = 5 * time.Second

// pollReadError marks reads that kept failing, so the failure is not mistaken for a timeout.
type pollReadError struct{ err error }

func (e *pollReadError) Error() string { return e.err.Error() }

// fatalRead: retrying cannot fix it. A 404 is not fatal: right after a create the read can lag behind.
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

// waitFor: each read has its own --timeout; the whole wait ends after timeout.
// known is the object as read before the wait, nil after a create; with it a 404 means the object is gone.
// It returns the last read and nil (done), a *pollReadError or a timeout error.
func waitFor[T any](
	ctx context.Context, f cmdutil.Factory, w io.Writer, timeout time.Duration, known *T,
	get func(context.Context) (*T, error), check func(*T) (status string, done bool),
) (*T, error) {
	last := known
	failures := 0
	status := "waiting"
	pollFn := func(ctx context.Context) (string, bool, error) {
		readCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
		defer cancel()
		v, err := get(readCtx)
		if err != nil {
			// The whole wait ran out during this read: a timeout, not a failed read.
			if ctx.Err() != nil {
				return status, false, nil
			}
			// Until a first read works, a 404 is a read lagging behind the create.
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
	return last, err
}

// waitWriter: the spinner line goes to stderr, and only for a human at a terminal.
func waitWriter(f cmdutil.Factory, ioStreams cmdutil.IOStreams) io.Writer {
	if f.OutputFormat() == outputTable && !f.AgentMode() && cmdutil.IsStderrTerminal() {
		return ioStreams.ErrOut
	}
	return nil
}

func clusterReady(c *verda.ManagedKubernetesCluster) (status string, done bool) {
	return c.Phase, c.Phase == verda.ManagedKubernetesClusterPhaseRunning && c.ExternalEndpoint != ""
}

type waitFailureInfo struct {
	Subject     string // for example "cluster prod in FIN-03"
	Timeout     time.Duration
	LastSeen    string
	TimeoutTail string // what to know or do after a timeout
	ReadNote    string // optional, after failed reads
	CheckCmd    string
}

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

func poolReasons(p *verda.ManagedKubernetesNodePool) []string {
	var reasons []string
	for i := range p.Errors {
		if !slices.Contains(reasons, p.Errors[i].Reason) {
			reasons = append(reasons, p.Errors[i].Reason)
		}
	}
	return reasons
}

// poolStatus is the line shown while waiting; reasons only while degraded.
func poolStatus(p *verda.ManagedKubernetesNodePool) string {
	s := p.State
	if reasons := poolReasons(p); p.State == verda.ManagedKubernetesNodePoolStateDegraded && len(reasons) > 0 {
		s += " (" + strings.Join(reasons, ", ") + ")"
	}
	return fmt.Sprintf("%s, %d/%d workers ready", s, p.ReadyReplicas, p.Replicas)
}

// poolCreated and poolScaled compare with the requested count, not the count
// read, so a stale first read cannot end the wait.
func poolCreated(want int) func(*verda.ManagedKubernetesNodePool) (string, bool) {
	return func(p *verda.ManagedKubernetesNodePool) (string, bool) {
		return poolStatus(p), p.State == verda.ManagedKubernetesNodePoolStateActive && p.ReadyReplicas == want
	}
}

func poolScaled(want int) func(*verda.ManagedKubernetesNodePool) (string, bool) {
	return func(p *verda.ManagedKubernetesNodePool) (string, bool) {
		done := p.State == verda.ManagedKubernetesNodePoolStateActive && p.Replicas == want && p.ReadyReplicas == want
		return poolStatus(p), done
	}
}

// writeResult returns waitErr after the structured result, so a failed wait
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

// wholeError keeps err's whole text as the message main prints. ClassifyError
// reduces a wrapped API error to the API's text, which drops the recovery advice
// around it (the ID, the check command, "workers keep billing"). Agents keep the
// wrapped error's code.
func wholeError(f cmdutil.Factory, err error) error {
	if !f.AgentMode() {
		return errors.New(err.Error())
	}
	ae := *cmdutil.ClassifyError(err)
	ae.Message = err.Error()
	return &ae
}

// waitForPool: known is the pool read before a scale, nil after a create (see waitFor).
func waitForPool(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, t *clusterTarget, name string, known *verda.ManagedKubernetesNodePool, done func(*verda.ManagedKubernetesNodePool) (string, bool), timeout time.Duration) (*verda.ManagedKubernetesNodePool, error) {
	if f.OutputFormat() == outputTable {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Node pool %s accepted; waiting for its workers (Ctrl+C stops waiting, the pool stays).\n", name)
	}
	return waitFor(ctx, f, waitWriter(f, ioStreams), timeout, known,
		func(ctx context.Context) (*verda.ManagedKubernetesNodePool, error) {
			return client.ManagedKubernetes.GetNodePool(ctx, t.Location, t.Name, name)
		}, done)
}

type poolWaitKind int

const (
	poolWaitCreate poolWaitKind = iota
	poolWaitScaleUp
	poolWaitScaleDown
	poolWaitSettle
)

// poolWaitFailure: while workers are being created, a timeout says they keep
// billing and how to stop them (delete after a create, scale back after a scale-up).
func poolWaitFailure(err error, kind poolWaitKind, t *clusterTarget, accepted, last *verda.ManagedKubernetesNodePool, timeout time.Duration, previous int) error {
	seen := accepted
	if last != nil {
		seen = last
	}
	describe := poolDescribeCmd(t, accepted.Name)
	subject := fmt.Sprintf("node pool %s in cluster %s (%s)", accepted.Name, t.Name, t.Location)
	creating := poolBackendKeepsTrying + " To stop: `" + poolCmd("delete", t, accepted.Name) + "`. Check with `" + describe + "`"
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
		info.TimeoutTail = poolBackendKeepsTrying + " To stop, scale back: `" + poolCmd("scale", t, accepted.Name) + " --replicas " + strconv.Itoa(previous) + " --yes`. Check with `" + describe + "`"
		info.ReadNote = poolBackendKeepsTrying
	default:
		info.TimeoutTail, info.ReadNote = creating, poolBackendKeepsTrying
	}
	return waitFailure(err, &info)
}

func poolSeen(p *verda.ManagedKubernetesNodePool) string {
	s := fmt.Sprintf("last state: %s, %d/%d workers ready", p.State, p.ReadyReplicas, p.Replicas)
	if reasons := poolReasons(p); len(reasons) > 0 {
		s += ", errors: " + strings.Join(reasons, ", ")
	}
	return s
}

// kubeconfigReady checks the YAML too: a read can say ready before it has it.
func kubeconfigReady(k *verda.ManagedKubernetesKubeconfig) (status string, done bool) {
	return k.Status, k.Status == verda.ManagedKubernetesKubeconfigStatusReady && k.Kubeconfig != ""
}
