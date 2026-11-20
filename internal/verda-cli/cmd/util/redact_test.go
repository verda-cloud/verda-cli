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

package util

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --debug dumps request/response bodies; every secret-shaped value the SDK or
// API puts on the wire must be redacted (review H1).

func TestRedactSensitiveBody_JSON(t *testing.T) {
	t.Parallel()

	secretKeys := []string{
		"client_secret", "secret_access_key", "service_account_key",
		"value_or_reference_to_secret", "base64_content", "docker_config_json",
		"jupyter_token", "access_token", "refresh_token", "id_token",
		"password", "api_key", "bearer", "authorization", "kubeconfig", "value",
	}
	for _, key := range secretKeys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			in := `{"` + key + `": "SUPERSECRET-VALUE", "ok": true}`
			got := redactSensitiveBody("application/json", in)
			if strings.Contains(got, "SUPERSECRET-VALUE") {
				t.Errorf("%q value leaked: %s", key, got)
			}
			if !strings.Contains(got, `"`+key+`": "<redacted>"`) {
				t.Errorf("%q not redacted in-place: %s", key, got)
			}
			if !strings.Contains(got, `"ok": true`) {
				t.Errorf("non-secret key mangled: %s", got)
			}
		})
	}

	t.Run("escaped quote inside value redacted whole", func(t *testing.T) {
		t.Parallel()
		in := `{"client_secret": "a\"b} tail"`
		got := redactSensitiveBody("application/json", in)
		if strings.Contains(got, `b} tail`) {
			t.Errorf("escaped-quote value leaked its remainder: %s", got)
		}
	})

	t.Run("kubeconfig YAML with escaped newlines and quotes redacted whole", func(t *testing.T) {
		t.Parallel()
		in := `{"id":"c-kc-1","kubeconfig":"apiVersion: v1\nusers:\n- user:\n    client-key-data: \"MARKER-KEY\"\n","status":"ready"}`
		got := redactSensitiveBody("application/json", in)
		if strings.Contains(got, "MARKER-KEY") || strings.Contains(got, "client-key-data") {
			t.Errorf("kubeconfig YAML leaked: %s", got)
		}
		if !strings.Contains(got, `"status":"ready"`) || !strings.Contains(got, `"id":"c-kc-1"`) {
			t.Errorf("neighboring fields mangled: %s", got)
		}
	})

	t.Run("unterminated value redacted to the end", func(t *testing.T) {
		t.Parallel()
		for name, in := range map[string]string{
			"kubeconfig":      `{"id":"c-kc-1","kubeconfig":"apiVersion: v1\nclient-key-data: MARKER-KEY`,
			"trailing escape": `{"kubeconfig":"client-key-data: MARKER-KEY\`,
			"client_secret":   `{"client_secret": "MARKER-KEY`,
		} {
			got := redactSensitiveBody("application/json", in)
			if strings.Contains(got, "MARKER-KEY") {
				t.Errorf("%s: unterminated value leaked: %s", name, got)
			}
		}
	})

	t.Run("kubeconfig key with spaces and a pretty-printed body", func(t *testing.T) {
		t.Parallel()
		in := "{\n  \"id\": \"c-kc-1\",\n  \"kubeconfig\" :  \"client-key-data: MARKER-KEY\\n\",\n  \"status\": \"ready\"\n}"
		got := redactSensitiveBody("application/json", in)
		if strings.Contains(got, "MARKER-KEY") {
			t.Errorf("spaced kubeconfig key leaked: %s", got)
		}
		if !strings.Contains(got, `"status": "ready"`) {
			t.Errorf("neighboring field mangled: %s", got)
		}
	})

	t.Run("spacing variants", func(t *testing.T) {
		t.Parallel()
		in := "{ \"client_secret\":\"x\",  \"refresh_token\"\t:  \"y\" }"
		got := redactSensitiveBody("application/json", in)
		if strings.Contains(got, `"x"`) || strings.Contains(got, `"y"`) {
			t.Errorf("compact/spaced JSON secrets leaked: %s", got)
		}
	})
}

func TestRedactSensitiveBody_Form(t *testing.T) {
	t.Parallel()

	// The SDK's form fallback shape: grant_type=client_credentials&client_id=…&client_secret=…
	in := "grant_type=client_credentials&client_id=my-id&client_secret=SUPERSECRET-VALUE" +
		"&refresh_token=REFRESH-SECRET&access_token=ACCESS-SECRET&password=PW-SECRET&token=TOK-SECRET"
	got := redactSensitiveBody("application/x-www-form-urlencoded", in)

	for _, leaked := range []string{"SUPERSECRET-VALUE", "REFRESH-SECRET", "ACCESS-SECRET", "PW-SECRET", "TOK-SECRET"} {
		if strings.Contains(got, leaked) {
			t.Errorf("form value %q leaked: %s", leaked, got)
		}
	}
	for _, marker := range []string{
		"client_secret=<redacted>", "refresh_token=<redacted>", "access_token=<redacted>",
		"password=<redacted>", "token=<redacted>",
	} {
		if !strings.Contains(got, marker) {
			t.Errorf("missing %q in: %s", marker, got)
		}
	}
	// Non-secret fields stay verbatim — debuggability is the whole point.
	for _, kept := range []string{"grant_type=client_credentials", "client_id=my-id"} {
		if !strings.Contains(got, kept) {
			t.Errorf("non-secret field %q mangled: %s", kept, got)
		}
	}
}

func TestRedactSensitiveBody_ContentTypeDispatch(t *testing.T) {
	t.Parallel()

	t.Run("form shape under json type stays as-is", func(t *testing.T) {
		t.Parallel()
		in := "client_secret=SUPERSECRET-VALUE"
		if got := redactSensitiveBody("application/json", in); got != in {
			t.Errorf("form body under JSON content type was mangled: %s", got)
		}
	})

	t.Run("json shape under form type stays as-is", func(t *testing.T) {
		t.Parallel()
		// Under-redaction is the security failure mode; mangling non-matching
		// bodies is the debuggability failure mode. The form pattern requires
		// key=value, which JSON never has, so the body passes through.
		in := `{"client_id": "my-id"}`
		if got := redactSensitiveBody("application/x-www-form-urlencoded", in); got != in {
			t.Errorf("json body under form content type was mangled: %s", got)
		}
	})

	t.Run("content type params and case tolerated", func(t *testing.T) {
		t.Parallel()
		in := "client_secret=SUPERSECRET-VALUE"
		got := redactSensitiveBody("Application/X-WWW-FORM-Urlencoded; charset=utf-8", in)
		if strings.Contains(got, "SUPERSECRET-VALUE") {
			t.Errorf("form body with params/case leaked: %s", got)
		}
	})

	t.Run("empty content type defaults to json", func(t *testing.T) {
		t.Parallel()
		in := `{"access_token": "TOK-SECRET"}`
		got := redactSensitiveBody("", in)
		if strings.Contains(got, "TOK-SECRET") {
			t.Errorf("json body with empty content type leaked: %s", got)
		}
	})
}

// debugTransport must keep a kubeconfig private key out of the debug log:
// bodies of /kubeconfigs endpoints are omitted, any other path is redacted
// whatever the key's case.
func TestDebugTransport_KubeconfigBodies(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","kubeconfig":"MARKER-KEY","Kubeconfig":"MARKER-KEY"}`)
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name, path, wantOut string
	}{
		{"kubeconfig endpoint omits bodies", "/v1/kubeconfigs/x", "<omitted: kubeconfig endpoint>"},
		{"other endpoint redacts keys in any case", "/v1/other", `"Kubeconfig":"<redacted>"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			client := &http.Client{Transport: &debugTransport{base: http.DefaultTransport, out: &out, enabled: func() bool { return true }}}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+tc.path, strings.NewReader(`{"kubeconfig":"MARKER-KEY"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if !bytes.Contains(body, []byte("MARKER-KEY")) {
				t.Fatalf("caller must still receive the body: %s", body)
			}
			if strings.Contains(out.String(), "MARKER-KEY") {
				t.Errorf("key leaked into debug output:\n%s", out.String())
			}
			if !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("debug output lacks %q:\n%s", tc.wantOut, out.String())
			}
		})
	}
}
