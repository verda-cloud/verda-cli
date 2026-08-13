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
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func marshalBoth(t *testing.T, v any) (jsonOut, yamlOut string) {
	t.Helper()
	var jb, yb bytes.Buffer
	if _, err := WriteStructured(&jb, "json", v); err != nil {
		t.Fatalf("json: %v", err)
	}
	if _, err := WriteStructured(&yb, "yaml", v); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return jb.String(), yb.String()
}

// A zero CreatedAt must vanish from both encodings — a pointer to a zero value
// would still marshal, so nilIfZero has to return nil, not &zero.
func TestSSHKeyViewOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	view := NewSSHKeyView(&verda.SSHKey{ID: "k-1", Name: "n", PublicKey: "ssh-ed25519 AAA"})

	if view.CreatedAt != nil {
		t.Fatalf("CreatedAt = %v, want nil for a zero time", view.CreatedAt)
	}
	jsonOut, yamlOut := marshalBoth(t, view)
	for name, out := range map[string]string{"json": jsonOut, "yaml": yamlOut} {
		if strings.Contains(out, "created_at") {
			t.Errorf("%s still carries created_at: %s", name, out)
		}
		if strings.Contains(out, "0001-01-01") {
			t.Errorf("%s emits a zero timestamp: %s", name, out)
		}
	}
}

// An empty fingerprint (the API sends null) must be absent, not "".
func TestSSHKeyViewOmitsEmptyFingerprint(t *testing.T) {
	t.Parallel()

	jsonOut, yamlOut := marshalBoth(t, NewSSHKeyView(&verda.SSHKey{ID: "k-1", Name: "n"}))
	if strings.Contains(jsonOut, "fingerprint") {
		t.Errorf("json carries an empty fingerprint: %s", jsonOut)
	}
	if strings.Contains(yamlOut, "fingerprint") {
		t.Errorf("yaml carries an empty fingerprint: %s", yamlOut)
	}
}

// A real timestamp round-trips unchanged, and the yaml tags keep the key
// snake_case — yaml/v3 ignores json tags and would emit "createdat" without them.
func TestSSHKeyViewKeepsRealValues(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 8, 11, 18, 51, 12, 577_000_000, time.UTC)
	view := NewSSHKeyView(&verda.SSHKey{
		ID: "k-1", Name: "n", PublicKey: "ssh-ed25519 AAA",
		Fingerprint: "SHA256:abc", CreatedAt: when,
	})

	if view.CreatedAt == nil || !view.CreatedAt.Equal(when) {
		t.Fatalf("CreatedAt = %v, want %v", view.CreatedAt, when)
	}
	jsonOut, yamlOut := marshalBoth(t, view)
	if !strings.Contains(jsonOut, "2026-08-11T18:51:12") {
		t.Errorf("json lost the timestamp: %s", jsonOut)
	}
	if !strings.Contains(yamlOut, "created_at") {
		t.Errorf("yaml key is not snake_case (missing yaml tag?): %s", yamlOut)
	}
	if strings.Contains(yamlOut, "createdat") {
		t.Errorf("yaml fell back to the field name: %s", yamlOut)
	}
}

func TestStartupScriptViewOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	view := NewStartupScriptView(&verda.StartupScript{ID: "s-1", Name: "boot", Script: "#!/bin/sh"})

	if view.CreatedAt != nil {
		t.Fatalf("CreatedAt = %v, want nil", view.CreatedAt)
	}
	jsonOut, yamlOut := marshalBoth(t, view)
	for name, out := range map[string]string{"json": jsonOut, "yaml": yamlOut} {
		if strings.Contains(out, "0001-01-01") {
			t.Errorf("%s emits a zero timestamp: %s", name, out)
		}
	}
	if !strings.Contains(jsonOut, "#!/bin/sh") {
		t.Errorf("script body was dropped: %s", jsonOut)
	}
}

func TestNewViewsPreserveOrderAndLength(t *testing.T) {
	t.Parallel()

	keys := []verda.SSHKey{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	views := NewSSHKeyViews(keys)
	if len(views) != 3 {
		t.Fatalf("len = %d, want 3", len(views))
	}
	for i, want := range []string{"a", "b", "c"} {
		if views[i].ID != want {
			t.Errorf("views[%d].ID = %q, want %q", i, views[i].ID, want)
		}
	}

	if got := NewSSHKeyViews(nil); len(got) != 0 {
		t.Errorf("nil input produced %d views", len(got))
	}
	if got := NewStartupScriptViews(nil); len(got) != 0 {
		t.Errorf("nil input produced %d views", len(got))
	}
}

// One row's absent timestamp must not affect another's.
func TestSSHKeyViewsMixed(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 8, 11, 18, 51, 12, 0, time.UTC)
	views := NewSSHKeyViews([]verda.SSHKey{{ID: "a"}, {ID: "b", CreatedAt: when}})

	if views[0].CreatedAt != nil {
		t.Errorf("row 0 gained a timestamp: %v", views[0].CreatedAt)
	}
	if views[1].CreatedAt == nil {
		t.Fatal("row 1 lost its timestamp")
	}
	jsonOut, _ := marshalBoth(t, views)
	if strings.Count(jsonOut, "created_at") != 1 {
		t.Errorf("created_at count = %d, want 1: %s", strings.Count(jsonOut, "created_at"), jsonOut)
	}
}

func TestTimeColumn(t *testing.T) {
	t.Parallel()

	if got := TimeColumn(nil, "2006-01-02"); got != "-" {
		t.Errorf("nil → %q, want %q", got, "-")
	}
	when := time.Date(2026, 8, 11, 18, 51, 0, 0, time.UTC)
	if got := TimeColumn(&when, "2006-01-02 15:04"); got != "2026-08-11 18:51" {
		t.Errorf("got %q", got)
	}
}

func TestTextColumn(t *testing.T) {
	t.Parallel()

	if got := TextColumn(""); got != "-" {
		t.Errorf("empty → %q, want %q", got, "-")
	}
	if got := TextColumn("SHA256:abc"); got != "SHA256:abc" {
		t.Errorf("got %q", got)
	}
}

// jsonKeys returns the json tag names declared on a struct type, ignoring
// options like ",omitempty".
func jsonKeys(t *testing.T, v any) map[string]bool {
	t.Helper()

	rt := reflect.TypeOf(v)
	keys := make(map[string]bool, rt.NumField())
	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		keys[strings.Split(tag, ",")[0]] = true
	}
	return keys
}

// A view that mirrors an SDK struct field-by-field silently drops any field the
// SDK adds later — and for the agent JSON contract, a silently missing field is
// a broken consumer. This test is the tripwire: it fails when verda.Instance
// grows a field InstanceView does not carry.
func TestInstanceViewCoversSDKFields(t *testing.T) {
	t.Parallel()

	sdk := jsonKeys(t, verda.Instance{})
	view := jsonKeys(t, InstanceView{})

	for key := range sdk {
		if !view[key] {
			t.Errorf("verda.Instance has json key %q that InstanceView drops — add it to the view", key)
		}
	}
	for key := range view {
		if !sdk[key] {
			t.Errorf("InstanceView invents json key %q that verda.Instance does not have", key)
		}
	}
}

func TestJobDeploymentShortViewCoversSDKFields(t *testing.T) {
	t.Parallel()

	sdk := jsonKeys(t, verda.JobDeploymentShortInfo{})
	view := jsonKeys(t, JobDeploymentShortView{})

	for key := range sdk {
		if !view[key] {
			t.Errorf("verda.JobDeploymentShortInfo has json key %q that the view drops", key)
		}
	}
	for key := range view {
		if !sdk[key] {
			t.Errorf("view invents json key %q", key)
		}
	}
}

func TestInstanceViewOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	gotJSON, gotYAML := marshalBoth(t, NewInstanceView(&verda.Instance{ID: "inst-1", Hostname: "box"}))

	if strings.Contains(gotJSON, "created_at") {
		t.Errorf("zero CreatedAt emitted in JSON: %s", gotJSON)
	}
	if !strings.Contains(gotJSON, `"hostname": "box"`) {
		t.Errorf("hostname lost: %s", gotJSON)
	}
	if strings.Contains(gotYAML, "0001-01-01") || strings.Contains(gotYAML, "createdat") {
		t.Errorf("YAML leaks a zero timestamp or an untagged key:\n%s", gotYAML)
	}
}

func TestInstanceViewKeepsRealCreatedAt(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 8, 11, 18, 51, 12, 0, time.UTC)
	gotJSON, gotYAML := marshalBoth(t, NewInstanceView(&verda.Instance{ID: "inst-1", CreatedAt: ts}))

	if !strings.Contains(gotJSON, `"created_at": "2026-08-11T18:51:12Z"`) {
		t.Errorf("real timestamp lost or reformatted: %s", gotJSON)
	}
	if !strings.Contains(gotYAML, "created_at:") {
		t.Errorf("YAML lost created_at:\n%s", gotYAML)
	}
}

func TestInstanceViewsPreserveOrderAndPerRowOmission(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 8, 11, 18, 51, 12, 0, time.UTC)
	views := NewInstanceViews([]verda.Instance{
		{ID: "a"},
		{ID: "b", CreatedAt: ts},
	})
	if len(views) != 2 || views[0].ID != "a" || views[1].ID != "b" {
		t.Fatalf("order or length changed: %+v", views)
	}
	if views[0].CreatedAt != nil {
		t.Errorf("row 0 gained a timestamp: %v", views[0].CreatedAt)
	}
	if views[1].CreatedAt == nil || !views[1].CreatedAt.Equal(ts) {
		t.Errorf("row 1 lost its timestamp: %v", views[1].CreatedAt)
	}
}

func TestJobDeploymentShortViewOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	gotJSON, gotYAML := marshalBoth(t, NewJobDeploymentShortView(&verda.JobDeploymentShortInfo{Name: "job-a"}))
	if strings.Contains(gotJSON, "created_at") {
		t.Errorf("zero CreatedAt emitted in JSON: %s", gotJSON)
	}
	if strings.Contains(gotYAML, "0001-01-01") {
		t.Errorf("zero CreatedAt emitted in YAML:\n%s", gotYAML)
	}

	ts := time.Date(2026, 8, 11, 18, 51, 12, 0, time.UTC)
	realJSON, _ := marshalBoth(t, NewJobDeploymentShortView(&verda.JobDeploymentShortInfo{Name: "job-b", CreatedAt: ts}))
	if !strings.Contains(realJSON, "2026-08-11T18:51:12Z") {
		t.Errorf("real timestamp lost: %s", realJSON)
	}
}

// fillNonZero recursively sets every settable field to a distinctive non-zero
// value so a copy can be compared field-by-field. time.Time is special-cased:
// its fields are unexported, so recursing into it would find nothing settable.
func fillNonZero(t *testing.T, v reflect.Value, n *int) {
	t.Helper()

	*n++
	switch v.Kind() {
	case reflect.String:
		v.SetString("s" + strconv.Itoa(*n))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(*n))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(*n))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(*n) + 0.25)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(t, v.Elem(), n)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := range 2 {
			fillNonZero(t, v.Index(i), n)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Date(2026, 8, 11, 18, 51, 12, 0, time.UTC)))
			return
		}
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				fillNonZero(t, f, n)
			}
		}
	default:
		// Maps, channels, funcs, interfaces: absent from these payloads.
	}
}

func marshalToMap(t *testing.T, v any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// A tag-name guard cannot see a field the view declares but the constructor
// never assigns — it marshals as a zero value and passes. Fill every SDK field
// with a distinctive value and compare the marshaled maps.
func TestInstanceViewCopiesEveryValue(t *testing.T) {
	t.Parallel()

	var inst verda.Instance
	n := 0
	fillNonZero(t, reflect.ValueOf(&inst).Elem(), &n)

	sdk := marshalToMap(t, inst)
	view := marshalToMap(t, NewInstanceView(&inst))

	for key, want := range sdk {
		got, ok := view[key]
		if !ok {
			t.Errorf("view dropped key %q", key)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("key %q: view has %#v, SDK has %#v", key, got, want)
		}
	}
	for key := range view {
		if _, ok := sdk[key]; !ok {
			t.Errorf("view invented key %q", key)
		}
	}
}

// On a zero-valued instance the view must drop created_at and nothing else: no
// other field may silently vanish from the agent contract.
func TestEmptyInstanceOnlyDropsCreatedAt(t *testing.T) {
	t.Parallel()

	sdk := marshalToMap(t, verda.Instance{})
	view := marshalToMap(t, NewInstanceView(&verda.Instance{}))

	var missing []string
	for key := range sdk {
		if _, ok := view[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) != 1 || missing[0] != "created_at" {
		t.Errorf("view drops %v on a zero instance; want exactly [created_at]", missing)
	}
	if len(view) != len(sdk)-1 {
		t.Errorf("view has %d keys, SDK has %d; want exactly one fewer", len(view), len(sdk))
	}
}
