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
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// The env endpoints key on the container inside the deployment ("<name>-0"),
// not the deployment: serverless.ContainerNameOf resolves it, and the tools
// never send the deployment name as container_name (production returns 404).
func (s *Server) registerContainerEnvTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("get_container_env",
			mcp.WithDescription("List the environment variables of a running container deployment (first container). Plain values are returned verbatim; secret-backed variables show the secret name, never the value."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
		),
		s.handleGetContainerEnv,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("set_container_env",
			mcp.WithDescription("Add or update environment variables on a running container deployment in place (no redeploy). New names are added, existing names are updated. Pass at least one of env / env_secrets. Plain values are visible to anyone who can describe the deployment: use env_secrets for anything sensitive. Returns the variables after the change."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
			mcp.WithArray("env", mcp.WithStringItems(), mcp.Description("Plain env vars as KEY=VALUE (KEY uppercase letters, digits, underscores)")),
			mcp.WithArray("env_secrets", mcp.WithStringItems(), mcp.Description("Secret-backed env vars as KEY=SECRET_NAME (secret from list_secrets)")),
		),
		s.handleSetContainerEnv,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("unset_container_env",
			mcp.WithDescription("Remove environment variables from a running container deployment (destructive: the running replicas lose them). REQUIRES confirm: true. Returns the variables after the change."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
			mcp.WithArray("keys", mcp.WithStringItems(), mcp.Required(), mcp.Description("Variable names to remove")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
		),
		s.handleUnsetContainerEnv,
	)
}

// envView is the env tools' result; env is the post-change read, not an echo of the input.
type envView struct {
	Deployment string                  `json:"deployment"`
	Container  string                  `json:"container"`
	Containers int                     `json:"containers"`
	Env        []verda.ContainerEnvVar `json:"env"`
	Added      *int                    `json:"added,omitempty"`
	Updated    *int                    `json:"updated,omitempty"`
	Removed    *int                    `json:"removed,omitempty"`
	Warning    string                  `json:"warning,omitempty"`
}

func newEnvView(deployment string, t *serverless.EnvTarget) envView {
	v := envView{Deployment: deployment, Container: t.Container, Containers: t.Containers, Env: t.Vars}
	if v.Env == nil {
		v.Env = []verda.ContainerEnvVar{}
	}
	if t.Containers > 1 {
		v.Warning = fmt.Sprintf("deployment has %d containers; showing %q", t.Containers, t.Container)
	}
	return v
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleGetContainerEnv(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := requiredString(args(req), "name")
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	target, err := serverless.LoadEnvTarget(ctx, client, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(newEnvView(name, target))
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleSetContainerEnv(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	r := &argReader{a: args(req)}
	name := r.reqStr("name")
	plain, secret := r.strs("env"), r.strs("env_secrets")
	if r.err != nil {
		return toolErrorResult(r.err), nil
	}
	if len(plain) == 0 && len(secret) == 0 {
		return toolErrorResult(cmdutil.NewMissingOneOfFlagsError(nil, []string{"env", "env_secrets"})), nil
	}
	vars, err := serverless.BuildEnvVars(plain, secret)
	if err != nil {
		return toolErrorResult(serverlessArgError(err)), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	target, err := serverless.LoadEnvTarget(ctx, client, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	toAdd, toUpdate := serverless.PartitionEnvVars(target.Vars, vars)
	if len(toAdd) > 0 {
		if err := client.ContainerDeployments.AddEnvironmentVariables(ctx, name,
			&verda.ContainerEnvVarsRequest{ContainerName: target.Container, Env: toAdd}); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}
	if len(toUpdate) > 0 {
		if err := client.ContainerDeployments.UpdateEnvironmentVariables(ctx, name,
			&verda.ContainerEnvVarsRequest{ContainerName: target.Container, Env: toUpdate}); err != nil {
			if len(toAdd) > 0 {
				return mcp.NewToolResultError(fmt.Sprintf("%d variable(s) were added but the update of %d existing variable(s) failed: %v", len(toAdd), len(toUpdate), err)), nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}
	}

	after, err := serverless.LoadEnvTarget(ctx, client, name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("variables were written but the re-read failed: %v", err)), nil
	}
	view := newEnvView(name, after)
	added, updated := len(toAdd), len(toUpdate)
	view.Added, view.Updated = &added, &updated
	return jsonResult(view)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleUnsetContainerEnv(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	r := &argReader{a: args(req)}
	name := r.reqStr("name")
	keys := r.strs("keys")
	confirm := r.boolean("confirm")
	if r.err != nil {
		return toolErrorResult(r.err), nil
	}
	if len(keys) == 0 {
		return toolErrorResult(missingArgError("keys")), nil
	}
	if !confirm {
		return toolErrorResult(confirmationRequiredError("unset_container_env")), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	before, err := serverless.LoadEnvTarget(ctx, client, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := client.ContainerDeployments.DeleteEnvironmentVariables(ctx, name,
		&verda.DeleteContainerEnvVarsRequest{ContainerName: before.Container, Env: keys}); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	after, err := serverless.LoadEnvTarget(ctx, client, name)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("variables were removed but the re-read failed: %v", err)), nil
	}
	view := newEnvView(name, after)
	removed := removedEnvCount(before.Vars, after.Vars)
	view.Removed = &removed
	return jsonResult(view)
}

// removedEnvCount is observed, not the request's length: names present before
// and absent after. A length difference would undercount when another client
// adds a variable between the two reads.
func removedEnvCount(before, after []verda.ContainerEnvVar) int {
	present := make(map[string]bool, len(after))
	for i := range after {
		present[after[i].Name] = true
	}
	n := 0
	for i := range before {
		if !present[before[i].Name] {
			n++
		}
	}
	return n
}
