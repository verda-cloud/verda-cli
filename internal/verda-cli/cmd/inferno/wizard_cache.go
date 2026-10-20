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

package inferno

import (
	"context"
	"fmt"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func withFetchSpinner[T any](ctx context.Context, status tui.Status, msg string, fn func(context.Context) (T, error)) (T, error) {
	return cmdutil.WithSpinner(ctx, status, msg, fn)
}

type clientFunc func() (*verda.Client, error)

type apiCache struct {
	computeResources []verda.ComputeResource
	registryCreds    []verda.RegistryCredentials
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
