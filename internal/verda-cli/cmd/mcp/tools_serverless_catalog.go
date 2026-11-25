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

package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

const (
	secretKindEnv  = "env"
	secretKindFile = "file"
	secretKindAll  = "all"
)

// Secret values never cross this surface: the API has no read-back, and the
// create tools are deliberately absent (a value would travel through the agent
// transcript). Deletes are a name plus a confirm gate.
func (s *Server) registerServerlessCatalogTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_serverless_compute",
			mcp.WithDescription("List serverless compute resources (GPU/CPU types and sizes) for container and batch-job deployments, with availability. Use the name as `compute` and the size as `compute_size` in create_container / create_batchjob."),
		),
		s.handleListServerlessCompute,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_registry_credentials",
			mcp.WithDescription("List container registry credentials by name, for `registry_credentials` on create_container / create_batchjob. Credentials are created with `verda container registry-creds create` or in the web console; their secrets are never returned."),
		),
		s.handleListRegistryCredentials,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_secrets",
			mcp.WithDescription("List serverless secrets by name: env secrets (for `env_secrets` KEY=SECRET_NAME) and file secrets (for `secret_mounts` SECRET:/path). Values are never returned. Secrets are created with `verda container secret create` / `file-secret create` or in the web console."),
			mcp.WithString("kind", mcp.Description("Which kind to list: env, file or all (default all)")),
		),
		s.handleListSecrets,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_secret",
			mcp.WithDescription("Delete a serverless env secret (irreversible). REQUIRES confirm: true. Fails if a deployment references it unless force is true."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Secret name")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithBoolean("force", mcp.Description("Delete even if a deployment still references the secret")),
		),
		s.deleteNamedHandler("delete_secret", func(ctx context.Context, c *verda.Client, name string, force bool) error {
			return c.ContainerDeployments.DeleteSecret(ctx, name, force)
		}),
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_file_secret",
			mcp.WithDescription("Delete a serverless file secret (irreversible). REQUIRES confirm: true. Fails if a deployment mounts it unless force is true."),
			mcp.WithString("name", mcp.Required(), mcp.Description("File secret name")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithBoolean("force", mcp.Description("Delete even if a deployment still mounts the file secret")),
		),
		s.deleteNamedHandler("delete_file_secret", func(ctx context.Context, c *verda.Client, name string, force bool) error {
			return c.ContainerDeployments.DeleteFileSecret(ctx, name, force)
		}),
	)

	s.mcpServer.AddTool(
		mcp.NewTool("delete_registry_credentials",
			mcp.WithDescription("Delete container registry credentials (irreversible). REQUIRES confirm: true. Fails if a deployment uses them unless force is true."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Registry credentials name")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithBoolean("force", mcp.Description("Delete even if a deployment still uses the credentials")),
		),
		s.deleteNamedHandler("delete_registry_credentials", func(ctx context.Context, c *verda.Client, name string, force bool) error {
			return c.ContainerDeployments.DeleteRegistryCredentials(ctx, name, force)
		}),
	)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListServerlessCompute(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	resources, err := client.ContainerDeployments.GetServerlessComputeResources(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(resources)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListRegistryCredentials(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	creds, err := client.ContainerDeployments.GetRegistryCredentials(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(creds)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListSecrets(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kind, err := optionalEnum(args(req), "kind", secretKindEnv, secretKindFile, secretKindAll)
	if err != nil {
		return toolErrorResult(err), nil
	}
	if kind == "" {
		kind = secretKindAll
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	result := map[string]any{}
	if kind != secretKindFile {
		secrets, err := client.ContainerDeployments.GetSecrets(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result["secrets"] = secrets
	}
	if kind != secretKindEnv {
		fileSecrets, err := client.ContainerDeployments.GetFileSecrets(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result["file_secrets"] = fileSecrets
	}
	return jsonResult(result)
}

type namedDeleteFn func(ctx context.Context, c *verda.Client, name string, force bool) error

// deleteNamedHandler: name + confirm gate (+ force) → DELETE; the gate runs before any API call.
func (s *Server) deleteNamedHandler(tool string, del namedDeleteFn) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		r := &argReader{a: args(req)}
		name := r.reqStr("name")
		confirm := r.boolean("confirm")
		force := r.boolean("force")
		if r.err != nil {
			return toolErrorResult(r.err), nil
		}
		if !confirm {
			return toolErrorResult(confirmationRequiredError(tool)), nil
		}

		client, err := s.verdaClient()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ctx, cancel := s.apiContext(ctx)
		defer cancel()

		if err := del(ctx, client, name, force); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(map[string]any{"name": name, "deleted": true, "force": force})
	}
}
