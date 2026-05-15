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
	"context"
	"fmt"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
)

// withFetchSpinner runs fn while showing a spinner labeled msg. If status is
// nil (e.g. tests with no TUI) or the spinner can't start, fn still runs.
// Used by wizard loaders so the API calls hidden inside cache fetchers
// (compute resources, registry creds, secrets) show progress instead of
// looking like a hang while the API responds.
func withFetchSpinner[T any](ctx context.Context, status tui.Status, msg string, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	if status == nil {
		return fn(ctx)
	}
	sp, err := status.Spinner(ctx, msg)
	if err != nil {
		return fn(ctx)
	}
	res, ferr := fn(ctx)
	if ferr != nil {
		sp.Stop("")
		return zero, ferr
	}
	sp.Stop("")
	return res, nil
}

// clientFunc lazily resolves a Verda API client. Early wizard steps (name,
// image, port, replicas) run without credentials; the client is dialed only
// when an API-dependent step fires.
type clientFunc func() (*verda.Client, error)

// apiCache holds data fetched during a wizard session so back-navigation
// doesn't trigger redundant API calls. All fields are populated lazily.
type apiCache struct {
	computeResources []verda.ComputeResource
	registryCreds    []verda.RegistryCredentials
	secrets          []verda.Secret
	fileSecrets      []verda.FileSecret
}

func (c *apiCache) fetchComputeResources(ctx context.Context, getClient clientFunc) ([]verda.ComputeResource, error) {
	if c.computeResources != nil {
		return c.computeResources, nil
	}
	client, err := getClient()
	if err != nil {
		return nil, err
	}
	res, err := client.ContainerDeployments.GetServerlessComputeResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching compute resources: %w", err)
	}
	c.computeResources = res
	return res, nil
}

func (c *apiCache) fetchRegistryCreds(ctx context.Context, getClient clientFunc) ([]verda.RegistryCredentials, error) {
	if c.registryCreds != nil {
		return c.registryCreds, nil
	}
	client, err := getClient()
	if err != nil {
		return nil, err
	}
	res, err := client.ContainerDeployments.GetRegistryCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching registry credentials: %w", err)
	}
	c.registryCreds = res
	return res, nil
}

func (c *apiCache) fetchSecrets(ctx context.Context, getClient clientFunc) ([]verda.Secret, error) {
	if c.secrets != nil {
		return c.secrets, nil
	}
	client, err := getClient()
	if err != nil {
		return nil, err
	}
	res, err := client.ContainerDeployments.GetSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching secrets: %w", err)
	}
	c.secrets = res
	return res, nil
}

func (c *apiCache) fetchFileSecrets(ctx context.Context, getClient clientFunc) ([]verda.FileSecret, error) {
	if c.fileSecrets != nil {
		return c.fileSecrets, nil
	}
	client, err := getClient()
	if err != nil {
		return nil, err
	}
	res, err := client.ContainerDeployments.GetFileSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching file secrets: %w", err)
	}
	c.fileSecrets = res
	return res, nil
}
