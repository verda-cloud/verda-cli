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
	"errors"
	"net/netip"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func prefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

func TestValidateSubnetCIDR(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in    string
		valid bool
	}{
		{"10.0.1.0/24", true},
		{"172.16.0.0/12", true},
		{"192.168.10.0/29", true},
		{"10.0.0.0/8", true},
		{"10.0.1.0/30", false},      // smaller than /29
		{"10.0.1.5/24", false},      // not canonical
		{"8.8.8.0/24", false},       // public
		{"172.32.0.0/16", false},    // outside 172.16/12
		{"10.0.0.0/7", false},       // larger than the private range
		{"fd00::/64", false},        // IPv6
		{"not-a-cidr", false},       // invalid
		{"", false},                 // empty
		{"192.168.0.0/15", false},   // wider than 192.168/16
		{" 10.0.2.0/24 ", true},     // whitespace trimmed
		{"172.16.255.248/29", true}, // smallest allowed
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateSubnetCIDR(tt.in)
			if (err == nil) != tt.valid {
				t.Errorf("ValidateSubnetCIDR(%q) err = %v, want valid=%v", tt.in, err, tt.valid)
			}
		})
	}
}

func TestPrefixesOverlap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want bool
	}{
		{"10.0.0.0/24", "10.0.0.0/24", true},
		{"10.0.0.0/16", "10.0.5.0/24", true},
		{"10.0.5.0/24", "10.0.0.0/16", true},
		{"10.0.0.0/24", "10.0.1.0/24", false},
		{"10.0.0.0/25", "10.0.0.128/25", false},
	}
	for _, tt := range tests {
		if got := PrefixesOverlap(netip.MustParsePrefix(tt.a), netip.MustParsePrefix(tt.b)); got != tt.want {
			t.Errorf("PrefixesOverlap(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestFreeSubnetCIDR(t *testing.T) {
	t.Parallel()
	pool := netip.MustParsePrefix("10.0.0.0/16")
	tests := []struct {
		name    string
		taken   []string
		within  netip.Prefix
		bits    int
		want    string
		wantErr bool
	}{
		{name: "empty network", within: pool, bits: 24, want: "10.0.0.0/24"},
		{name: "first taken", taken: []string{"10.0.0.0/24"}, within: pool, bits: 24, want: "10.0.1.0/24"},
		{name: "gap between subnets", taken: []string{"10.0.0.0/24", "10.0.2.0/24"}, within: pool, bits: 24, want: "10.0.1.0/24"},
		{name: "larger subnet covers several blocks", taken: []string{"10.0.0.0/22"}, within: pool, bits: 24, want: "10.0.4.0/24"},
		{name: "small subnet blocks its /24", taken: []string{"10.0.0.8/29"}, within: pool, bits: 24, want: "10.0.1.0/24"},
		{name: "subnet outside the pool is ignored", taken: []string{"172.16.0.0/24"}, within: pool, bits: 24, want: "10.0.0.0/24"},
		{name: "supernet of the pool", taken: []string{"10.0.0.0/8"}, within: pool, bits: 24, wantErr: true},
		{name: "full range", taken: []string{"10.0.0.0/17", "10.0.128.0/17"}, within: pool, bits: 24, wantErr: true},
		{name: "IPv6 range", within: netip.MustParsePrefix("fd00::/48"), bits: 64, wantErr: true},
		{name: "invalid range", within: netip.Prefix{}, bits: 24, wantErr: true},
		{name: "block larger than range", within: pool, bits: 8, wantErr: true},
		{name: "IPv6 taken is ignored", taken: []string{"fd00::/64"}, within: pool, bits: 24, want: "10.0.0.0/24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := FreeSubnetCIDR(prefixes(t, tt.taken...), tt.within, tt.bits)
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

func TestFreeSubnetCIDRFullRangeIsErrNoFreeSubnet(t *testing.T) {
	t.Parallel()
	_, err := FreeSubnetCIDR(prefixes(t, "10.0.0.0/16"), DefaultSubnetPool, DefaultSubnetBits)
	if !errors.Is(err, ErrNoFreeSubnet) {
		t.Fatalf("err = %v, want ErrNoFreeSubnet", err)
	}
}

func TestDefaultNetwork(t *testing.T) {
	t.Parallel()
	a := verda.PrivateNetwork{ID: "pn_a", Name: "a"}
	b := verda.PrivateNetwork{ID: "pn_b", Name: "b"}
	bDefault := b
	bDefault.IsDefault = true
	aDefault := a
	aDefault.IsDefault = true

	tests := []struct {
		name     string
		networks []verda.PrivateNetwork
		want     string
	}{
		{name: "none"},
		{name: "one default", networks: []verda.PrivateNetwork{aDefault}, want: "pn_a"},
		{name: "one not default is not picked", networks: []verda.PrivateNetwork{a}},
		{name: "several with a default", networks: []verda.PrivateNetwork{a, bDefault}, want: "pn_b"},
		{name: "several without a default", networks: []verda.PrivateNetwork{a, b}},
	}
	for _, tt := range tests {
		got := DefaultNetwork(tt.networks)
		gotID := ""
		if got != nil {
			gotID = got.ID
		}
		if gotID != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, gotID, tt.want)
		}
	}
}

func TestDefaultSubnet(t *testing.T) {
	t.Parallel()
	sub := func(id, loc string, def bool) verda.Subnet {
		return verda.Subnet{ID: id, Location: verda.Location{Code: loc}, IsDefault: def}
	}
	tests := []struct {
		name    string
		subnets []verda.Subnet
		loc     string
		want    string
		wantErr error
	}{
		{name: "none", loc: "FIN-03", wantErr: ErrNoSubnetInLocation},
		{name: "none in this location", subnets: []verda.Subnet{sub("sn_1", "FIN-01", true)}, loc: "FIN-03", wantErr: ErrNoSubnetInLocation},
		{name: "one, not default", subnets: []verda.Subnet{sub("sn_1", "FIN-03", false)}, loc: "FIN-03", want: "sn_1"},
		{name: "several with a default", subnets: []verda.Subnet{sub("sn_1", "FIN-03", false), sub("sn_2", "FIN-03", true)}, loc: "FIN-03", want: "sn_2"},
		{name: "several without a default", subnets: []verda.Subnet{sub("sn_1", "FIN-03", false), sub("sn_2", "FIN-03", false)}, loc: "FIN-03", wantErr: ErrAmbiguousSubnet},
		{name: "location match ignores case", subnets: []verda.Subnet{sub("sn_1", "FIN-03", false)}, loc: "fin-03", want: "sn_1"},
		{name: "default elsewhere does not count", subnets: []verda.Subnet{sub("sn_1", "FIN-01", true), sub("sn_2", "FIN-03", false), sub("sn_3", "FIN-03", false)}, loc: "FIN-03", wantErr: ErrAmbiguousSubnet},
	}
	for _, tt := range tests {
		got, err := DefaultSubnet(tt.subnets, tt.loc)
		if tt.wantErr != nil {
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("%s: err = %v, want %v", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got == nil || got.ID != tt.want {
			t.Errorf("%s: got %v (err %v), want %s", tt.name, got, err, tt.want)
		}
	}
}
