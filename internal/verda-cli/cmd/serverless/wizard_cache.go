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
)

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
