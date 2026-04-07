package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func (s *Server) registerSSHTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_ssh_keys",
			mcp.WithDescription("List SSH keys. Use 'search' to filter by name or email to quickly find a specific key instead of listing all."),
			mcp.WithString("search", mcp.Description("Filter keys by name (case-insensitive substring match)")),
		),
		s.handleListSSHKeys,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("add_ssh_key",
			mcp.WithDescription("Add a new SSH public key to the Verda Cloud account"),
			mcp.WithString("name", mcp.Required(), mcp.Description("Name for the SSH key")),
			mcp.WithString("public_key", mcp.Required(), mcp.Description("SSH public key content (e.g. ssh-ed25519 AAAA...)")),
		),
		s.handleAddSSHKey,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_ssh_command",
			mcp.WithDescription("Get the SSH command to connect to a VM instance"),
			mcp.WithString("id_or_hostname", mcp.Required(), mcp.Description("Instance ID or hostname")),
			mcp.WithString("user", mcp.Description("SSH user (default root)")),
			mcp.WithString("key_path", mcp.Description("Path to SSH identity file")),
		),
		s.handleGetSSHCommand,
	)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListSSHKeys(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	keys, err := client.SSHKeys.GetAllSSHKeys(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if search := optionalString(args(req), "search"); search != "" {
		lower := strings.ToLower(search)
		filtered := keys[:0]
		for i := range keys {
			if strings.Contains(strings.ToLower(keys[i].Name), lower) {
				filtered = append(filtered, keys[i])
			}
		}
		keys = filtered
	}

	return jsonResult(keys)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleAddSSHKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	name, err := requiredString(args(req), "name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	publicKey, err := requiredString(args(req), "public_key")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	key, err := client.SSHKeys.AddSSHKey(ctx, &verda.CreateSSHKeyRequest{
		Name:      name,
		PublicKey: publicKey,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(key)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleGetSSHCommand(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if _, err := s.verdaClient(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	idOrHostname, err := requiredString(args(req), "id_or_hostname")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	user := optionalString(args(req), "user")
	if user == "" {
		user = "root"
	}
	keyPath := optionalString(args(req), "key_path")

	// Try to resolve the instance to get the IP.
	inst, err := s.resolveInstance(ctx, idOrHostname)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if inst.IP == nil || *inst.IP == "" {
		return mcp.NewToolResultError(fmt.Sprintf("instance %q has no IP address (status: %s)", inst.Hostname, inst.Status)), nil
	}

	cmd := fmt.Sprintf("ssh %s@%s", user, *inst.IP)
	if keyPath != "" {
		cmd = fmt.Sprintf("ssh -i %s %s@%s", keyPath, user, *inst.IP)
	}

	result := map[string]string{
		"command":  cmd,
		"host":     *inst.IP,
		"user":     user,
		"hostname": inst.Hostname,
		"status":   inst.Status,
	}
	return jsonResult(result)
}

// resolveInstance finds an instance by ID or hostname.
func (s *Server) resolveInstance(ctx context.Context, idOrHostname string) (*verda.Instance, error) {
	client, err := s.verdaClient()
	if err != nil {
		return nil, err
	}

	// Try by ID first.
	inst, err := client.Instances.GetByID(ctx, idOrHostname)
	if err == nil {
		return inst, nil
	}

	// Fall back to searching by hostname.
	instances, err := client.Instances.Get(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range instances {
		if instances[i].Hostname == idOrHostname {
			return &instances[i], nil
		}
	}
	return nil, fmt.Errorf("instance %q not found", idOrHostname)
}

// resolveSSHKeyIDs takes a list of SSH key IDs or names and returns resolved IDs.
// If an input looks like an existing ID it's kept as-is; otherwise it's matched
// by name (case-insensitive substring).
func (s *Server) resolveSSHKeyIDs(ctx context.Context, client *verda.Client, inputs []string) ([]string, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	keys, err := client.SSHKeys.GetAllSSHKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching SSH keys: %w", err)
	}

	// Build lookup maps.
	byID := make(map[string]bool, len(keys))
	for i := range keys {
		byID[keys[i].ID] = true
	}

	var resolved []string
	for _, input := range inputs {
		// Already a valid ID.
		if byID[input] {
			resolved = append(resolved, input)
			continue
		}
		// Search by name.
		lower := strings.ToLower(input)
		found := false
		for i := range keys {
			if strings.Contains(strings.ToLower(keys[i].Name), lower) {
				resolved = append(resolved, keys[i].ID)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("SSH key %q not found (searched by ID and name)", input)
		}
	}
	return resolved, nil
}

// defaultSSHKeyID returns the most recently created SSH key ID.
// Returns empty string if no keys exist.
func (s *Server) defaultSSHKeyID(ctx context.Context, client *verda.Client) (string, error) {
	keys, err := client.SSHKeys.GetAllSSHKeys(ctx)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", nil
	}
	// Keys are returned in order; pick the last one (most recent).
	return keys[len(keys)-1].ID, nil
}
