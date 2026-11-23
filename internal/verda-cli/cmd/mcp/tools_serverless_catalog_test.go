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
	"net/http"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestListServerlessCompute(t *testing.T) {
	t.Parallel()
	s, _ := newMockBackedServer(t)

	var resources []verda.ComputeResource
	mustJSON(t, resultText(t, call(t, s.handleListServerlessCompute, "list_serverless_compute", map[string]any{})), &resources)
	if len(resources) != 4 || resources[0].Name == "" {
		t.Fatalf("compute resources = %v", resources)
	}
}

func TestListSecretsNamesOnly(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedSecret("prod-key")
	mock.SeedFileSecret("certs", "tls.crt", "tls.key")
	mock.SeedRegistryCredentials("ghcr")

	res := call(t, s.handleListSecrets, "list_secrets", map[string]any{})
	text := resultText(t, res)
	out := parseSuccess(t, res)
	if got := dig(t, out, "secrets", 0, "name"); got != "prod-key" {
		t.Errorf("secrets[0].name = %v", got)
	}
	if got := dig(t, out, "file_secrets", 0, "file_names", 1); got != "tls.key" {
		t.Errorf("file_secrets[0].file_names[1] = %v", got)
	}
	if strings.Contains(text, `"value"`) {
		t.Fatalf("list_secrets must never carry a value field:\n%s", text)
	}

	out = parseSuccess(t, call(t, s.handleListSecrets, "list_secrets", map[string]any{"kind": "env"}))
	if _, hasFiles := out["file_secrets"]; hasFiles {
		t.Error("kind=env must not list file secrets")
	}
	assertFieldError(t, call(t, s.handleListSecrets, "list_secrets", map[string]any{"kind": "tls"}), "kind")

	var creds []verda.RegistryCredentials
	mustJSON(t, resultText(t, call(t, s.handleListRegistryCredentials, "list_registry_credentials", map[string]any{})), &creds)
	if len(creds) != 1 || creds[0].Name != "ghcr" {
		t.Fatalf("registry credentials = %v", creds)
	}
}

func TestDeleteCredentialsConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedSecret("prod-key")
	mock.SeedFileSecret("certs", "tls.crt")
	mock.SeedRegistryCredentials("ghcr")

	delSecret := s.deleteNamedHandler("delete_secret", func(ctx context.Context, c *verda.Client, name string, force bool) error {
		return c.ContainerDeployments.DeleteSecret(ctx, name, force)
	})
	assertToolErrorCode(t, call(t, delSecret, "delete_secret", map[string]any{"name": "prod-key"}), "CONFIRMATION_REQUIRED")
	if mock.RequestCount(http.MethodDelete, "/secrets/prod-key") != 0 || !mock.HasSecret("prod-key") {
		t.Fatal("refused delete must have zero side effects")
	}
	out := parseSuccess(t, call(t, delSecret, "delete_secret", map[string]any{"name": "prod-key", "confirm": true, "force": true}))
	if out["deleted"] != true || mock.HasSecret("prod-key") {
		t.Fatalf("delete_secret = %v; still present: %v", out, mock.HasSecret("prod-key"))
	}
	if q := mock.DeleteQueries(); len(q) != 1 || q[0] != "force=true" {
		t.Fatalf("force must reach the wire as a query; got %q", q)
	}

	delFile := s.deleteNamedHandler("delete_file_secret", func(ctx context.Context, c *verda.Client, name string, force bool) error {
		return c.ContainerDeployments.DeleteFileSecret(ctx, name, force)
	})
	parseSuccess(t, call(t, delFile, "delete_file_secret", map[string]any{"name": "certs", "confirm": true}))
	if mock.HasFileSecret("certs") {
		t.Error("file secret not deleted")
	}

	delCreds := s.deleteNamedHandler("delete_registry_credentials", func(ctx context.Context, c *verda.Client, name string, force bool) error {
		return c.ContainerDeployments.DeleteRegistryCredentials(ctx, name, force)
	})
	parseSuccess(t, call(t, delCreds, "delete_registry_credentials", map[string]any{"name": "ghcr", "confirm": true}))
	if mock.HasRegistryCredentials("ghcr") {
		t.Error("registry credentials not deleted")
	}
	if res := call(t, delCreds, "delete_registry_credentials", map[string]any{"name": "ghcr", "confirm": true}); !res.IsError {
		t.Error("deleting a missing name must be a tool error")
	}
}
