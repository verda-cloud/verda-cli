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
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// maxSubnetBits is the smallest subnet the API accepts (/29).
const maxSubnetBits = 29

// defaultSubnetPool is where freeSubnetCIDR looks for a subnet range.
var defaultSubnetPool = netip.MustParsePrefix("10.0.0.0/16")

// defaultSubnetBits is the size of a suggested subnet (/24).
const defaultSubnetBits = 24

var privateRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

// validateSubnetCIDR mirrors the API rules: canonical IPv4, /29 or larger, inside an RFC 1918 range.
func validateSubnetCIDR(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("%q is not an IPv4 CIDR (example: 10.0.1.0/24)", s)
	}
	if p != p.Masked() {
		return netip.Prefix{}, fmt.Errorf("%q is not canonical; use %s", s, p.Masked())
	}
	if p.Bits() > maxSubnetBits {
		return netip.Prefix{}, fmt.Errorf("%q is too small; the smallest subnet is /%d", s, maxSubnetBits)
	}
	for _, r := range privateRanges {
		if r.Bits() <= p.Bits() && r.Contains(p.Addr()) {
			return p, nil
		}
	}
	return netip.Prefix{}, fmt.Errorf("%q must be inside 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16", s)
}

// freeSubnetCIDR returns the first /24 in 10.0.0.0/16 that overlaps none of taken.
func freeSubnetCIDR(taken []netip.Prefix) (netip.Prefix, error) {
	for a := defaultSubnetPool.Addr(); defaultSubnetPool.Contains(a); {
		block := netip.PrefixFrom(a, defaultSubnetBits)
		if !slices.ContainsFunc(taken, block.Overlaps) {
			return block, nil
		}
		for block.Contains(a) {
			a = a.Next()
		}
	}
	return netip.Prefix{}, fmt.Errorf("no free subnet range: every /%d in %s is taken", defaultSubnetBits, defaultSubnetPool)
}

// subnetPrefixes skips subnets whose CIDR does not parse.
func subnetPrefixes(subnets []verda.Subnet) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(subnets))
	for i := range subnets {
		if p, err := netip.ParsePrefix(subnets[i].CIDR); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// defaultNetwork returns nil when no network is the default; it never falls back to another network.
func defaultNetwork(networks []verda.PrivateNetwork) *verda.PrivateNetwork {
	for i := range networks {
		if networks[i].IsDefault {
			return &networks[i]
		}
	}
	return nil
}
