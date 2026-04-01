package util

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	_ "github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// Factory provides shared resources that are created once in the root command
// and passed down to every subcommand. This pattern keeps commands testable
// and shared configuration in one place.
type Factory interface {
	// ServerAddr returns the configured API server address.
	ServerAddr() string
	// HTTPClient returns a shared HTTP client with the configured timeout.
	HTTPClient() *http.Client
	// Options returns the underlying Options for advanced use.
	Options() *clioptions.Options
	// VerdaClient returns a configured Verda SDK client.
	VerdaClient() (*verda.Client, error)
	// Login authenticates and returns a bearer token.
	Login() (string, error)
	// Token resolves a bearer token using the best available method.
	Token() string
	// Prompter returns the interactive prompt interface.
	Prompter() tui.Prompter
	// Status returns the status/output display interface.
	Status() tui.Status
	// Debug returns true if --debug is enabled.
	Debug() bool
}

type factoryImpl struct {
	opts     *clioptions.Options
	client   *http.Client
	prompter tui.Prompter
	status   tui.Status
	verda    *verda.Client
}

// NewFactory creates a Factory from the given Options.
func NewFactory(opts *clioptions.Options) Factory {
	return &factoryImpl{
		opts:     opts,
		client:   &http.Client{Timeout: opts.Timeout},
		prompter: tui.Default(),
		status:   tui.DefaultStatus(),
	}
}

func (f *factoryImpl) ServerAddr() string           { return f.opts.Server }
func (f *factoryImpl) HTTPClient() *http.Client     { return f.client }
func (f *factoryImpl) Options() *clioptions.Options { return f.opts }
func (f *factoryImpl) Prompter() tui.Prompter       { return f.prompter }
func (f *factoryImpl) Status() tui.Status           { return f.status }
func (f *factoryImpl) Debug() bool                  { return f.opts.Debug }

// VerdaClient creates or reuses the shared Verda SDK client.
func (f *factoryImpl) VerdaClient() (*verda.Client, error) {
	if f.verda != nil {
		return f.verda, nil
	}

	auth := f.opts.AuthOptions
	if auth.ClientID == "" || auth.ClientSecret == "" {
		return nil, fmt.Errorf("no credentials configured\n\n" +
			"Run \"verda auth login\" to set up your credentials, or provide them via:\n" +
			"  --auth.client-id / VERDA_CLIENT_ID\n" +
			"  --auth.client-secret / VERDA_CLIENT_SECRET")
	}

	options := []verda.ClientOption{
		verda.WithBaseURL(f.opts.Server),
		verda.WithClientID(auth.ClientID),
		verda.WithClientSecret(auth.ClientSecret),
		verda.WithHTTPClient(f.client),
		verda.WithUserAgent("verda"),
	}
	if auth.BearerToken != "" {
		options = append(options, verda.WithAuthBearerToken(auth.BearerToken))
	}

	client, err := verda.NewClient(options...)
	if err != nil {
		return nil, err
	}

	f.verda = client
	return client, nil
}

// Login performs authentication against the API server.
func (f *factoryImpl) Login() (string, error) {
	client, err := f.VerdaClient()
	if err != nil {
		return "", err
	}

	token, err := client.Auth.GetBearerToken()
	if err != nil {
		return "", err
	}

	return strings.TrimPrefix(token, "Bearer "), nil
}

// Token resolves a bearer token using the best available method.
func (f *factoryImpl) Token() string {
	auth := f.opts.AuthOptions
	if auth.BearerToken != "" {
		return auth.BearerToken
	}
	if auth.ClientID != "" && auth.ClientSecret != "" {
		token, _ := f.Login()
		return token
	}
	return ""
}
