package util

import (
	"errors"
	"net/http"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// TestFactory is a configurable Factory for use in tests. It implements the
// Factory interface with sensible defaults and allows overriding any field.
type TestFactory struct {
	// PrompterOverride sets the prompter. Defaults to tuitest.New().
	PrompterOverride tui.Prompter

	// StatusOverride sets the status. Defaults to nil (no spinners).
	StatusOverride tui.Status

	// ClientOverride sets the Verda client. If nil, VerdaClient() returns ErrNoClient.
	ClientOverride *verda.Client

	// OutputFormatOverride sets the output format. Defaults to "table".
	OutputFormatOverride string

	// DebugOverride enables debug output.
	DebugOverride bool

	// OptionsOverride sets custom options. If nil, sensible defaults are used.
	OptionsOverride *clioptions.Options
}

// ErrNoClient is returned by TestFactory.VerdaClient when no client is configured.
var ErrNoClient = errors.New("test: no Verda client configured")

// NewTestFactory creates a TestFactory with a mock prompter ready for use.
func NewTestFactory(mock *tuitest.Prompter) *TestFactory {
	return &TestFactory{
		PrompterOverride: mock,
	}
}

func (f *TestFactory) ServerAddr() string { return "https://test.verda.com/v1" }

func (f *TestFactory) HTTPClient() *http.Client { return &http.Client{Timeout: 10 * time.Second} }

func (f *TestFactory) Options() *clioptions.Options {
	if f.OptionsOverride != nil {
		return f.OptionsOverride
	}
	return &clioptions.Options{
		Server:  "https://test.verda.com/v1",
		Timeout: 10 * time.Second,
		Output:  f.OutputFormat(),
		Debug:   f.DebugOverride,
	}
}

func (f *TestFactory) VerdaClient() (*verda.Client, error) {
	if f.ClientOverride != nil {
		return f.ClientOverride, nil
	}
	return nil, ErrNoClient
}

func (f *TestFactory) Login() (string, error) { return "", ErrNoClient }

func (f *TestFactory) Token() string { return "" }

func (f *TestFactory) Prompter() tui.Prompter {
	if f.PrompterOverride != nil {
		return f.PrompterOverride
	}
	return tuitest.New()
}

func (f *TestFactory) Status() tui.Status {
	return f.StatusOverride
}

func (f *TestFactory) Debug() bool { return f.DebugOverride }

func (f *TestFactory) OutputFormat() string {
	if f.OutputFormatOverride != "" {
		return f.OutputFormatOverride
	}
	return "table"
}
