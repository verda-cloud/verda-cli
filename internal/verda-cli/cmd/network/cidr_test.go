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

package network

import (
	"net/netip"
	"testing"
)

func prefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func TestFreeSubnetCIDR(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		taken   []string
		want    string
		wantErr bool
	}{
		{name: "empty network", want: "10.0.0.0/24"},
		{name: "first taken", taken: []string{"10.0.0.0/24"}, want: "10.0.1.0/24"},
		{name: "gap between subnets", taken: []string{"10.0.0.0/24", "10.0.2.0/24"}, want: "10.0.1.0/24"},
		{name: "larger subnet covers several blocks", taken: []string{"10.0.0.0/22"}, want: "10.0.4.0/24"},
		{name: "small subnet blocks its /24", taken: []string{"10.0.0.8/29"}, want: "10.0.1.0/24"},
		{name: "subnet outside the pool is ignored", taken: []string{"172.16.0.0/24"}, want: "10.0.0.0/24"},
		{name: "supernet of the pool", taken: []string{"10.0.0.0/8"}, wantErr: true},
		{name: "full range", taken: []string{"10.0.0.0/17", "10.0.128.0/17"}, wantErr: true},
		{name: "IPv6 taken is ignored", taken: []string{"fd00::/64"}, want: "10.0.0.0/24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := freeSubnetCIDR(prefixes(t, tt.taken...))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}
