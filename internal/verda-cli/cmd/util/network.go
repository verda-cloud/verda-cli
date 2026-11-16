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
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// MaxSubnetBits is the smallest subnet the API accepts (/29).
const MaxSubnetBits = 29

// DefaultSubnetPool is where FreeSubnetCIDR looks for a subnet range by default.
var DefaultSubnetPool = netip.MustParsePrefix("10.0.0.0/16")

// DefaultSubnetBits is the size of a suggested subnet (/24).
const DefaultSubnetBits = 24

var privateRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

var (
	// ErrNoSubnetInLocation: the network has no subnet in the location.
	ErrNoSubnetInLocation = errors.New("no subnet in this location")
	// ErrAmbiguousSubnet: several subnets in the location and none is the default.
	ErrAmbiguousSubnet = errors.New("several subnets in this location and none is the default")
	// ErrNoFreeSubnet: every block of the requested size overlaps a taken range.
	ErrNoFreeSubnet = errors.New("no free subnet range")
)

// ValidateSubnetCIDR mirrors the API rules: canonical IPv4, /29 or larger, inside an RFC 1918 range.
func ValidateSubnetCIDR(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("%q is not an IPv4 CIDR (example: 10.0.1.0/24)", s)
	}
	if p != p.Masked() {
		return netip.Prefix{}, fmt.Errorf("%q is not canonical; use %s", s, p.Masked())
	}
	if p.Bits() > MaxSubnetBits {
		return netip.Prefix{}, fmt.Errorf("%q is too small; the smallest subnet is /%d", s, MaxSubnetBits)
	}
	for _, r := range privateRanges {
		if r.Bits() <= p.Bits() && r.Contains(p.Addr()) {
			return p, nil
		}
	}
	return netip.Prefix{}, fmt.Errorf("%q must be inside 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16", s)
}

// PrefixesOverlap reports whether two prefixes share any address.
func PrefixesOverlap(a, b netip.Prefix) bool {
	return a.Overlaps(b)
}

// FreeSubnetCIDR returns the first /bits block inside within that overlaps none of taken.
func FreeSubnetCIDR(taken []netip.Prefix, within netip.Prefix, bits int) (netip.Prefix, error) {
	if !within.IsValid() || !within.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("range %q is not an IPv4 CIDR", within)
	}
	if bits < within.Bits() || bits > 32 {
		return netip.Prefix{}, fmt.Errorf("a /%d block does not fit in %s", bits, within)
	}
	within = within.Masked()
	size := uint64(1) << (32 - bits)
	start := ipv4ToUint(within.Addr())
	end := start + (uint64(1) << (32 - within.Bits()))
	for addr := start; addr < end; {
		block := netip.PrefixFrom(uintToIPv4(addr), bits)
		next := addr + size
		free := true
		for _, t := range taken {
			if !t.IsValid() || !t.Addr().Is4() || !t.Overlaps(block) {
				continue
			}
			free = false
			// Skip past a taken range larger than one block.
			if tEnd := ipv4ToUint(t.Masked().Addr()) + (uint64(1) << (32 - t.Bits())); tEnd > next {
				next = (tEnd + size - 1) / size * size
			}
		}
		if free {
			return block, nil
		}
		addr = next
	}
	return netip.Prefix{}, fmt.Errorf("%w: every /%d in %s is taken", ErrNoFreeSubnet, bits, within)
}

// SubnetPrefixes parses the CIDRs of subnets; unparsable ones are skipped.
func SubnetPrefixes(subnets []verda.Subnet) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(subnets))
	for i := range subnets {
		if p, err := netip.ParsePrefix(subnets[i].CIDR); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// DefaultNetwork returns the project's default network, or nil. It never picks a non-default one.
func DefaultNetwork(networks []verda.PrivateNetwork) *verda.PrivateNetwork {
	for i := range networks {
		if networks[i].IsDefault {
			return &networks[i]
		}
	}
	return nil
}

// DefaultSubnet returns the default subnet in location, else the only one there.
// Errors: ErrNoSubnetInLocation, ErrAmbiguousSubnet.
func DefaultSubnet(subnets []verda.Subnet, location string) (*verda.Subnet, error) {
	var inLocation []*verda.Subnet
	for i := range subnets {
		if strings.EqualFold(subnets[i].Location.Code, location) {
			inLocation = append(inLocation, &subnets[i])
		}
	}
	switch len(inLocation) {
	case 0:
		return nil, ErrNoSubnetInLocation
	case 1:
		return inLocation[0], nil
	}
	for _, s := range inLocation {
		if s.IsDefault {
			return s, nil
		}
	}
	return nil, ErrAmbiguousSubnet
}

func ipv4ToUint(a netip.Addr) uint64 {
	b := a.As4()
	return uint64(binary.BigEndian.Uint32(b[:]))
}

// uintToIPv4 keeps the low 32 bits of v.
func uintToIPv4(v uint64) netip.Addr {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return netip.AddrFrom4([4]byte(b[4:]))
}

// ResolveNetwork finds a network by ID or name.
func ResolveNetwork(ctx context.Context, client *verda.Client, idOrName string) (*verda.PrivateNetwork, error) {
	networks, err := client.PrivateNetworks.Get(ctx)
	if err != nil {
		return nil, err
	}
	if n := FindNetwork(networks, idOrName); n != nil {
		return n, nil
	}
	return nil, NewNotFoundError("network", idOrName)
}

// FindNetwork matches by ID first, then by name (names are unique in a project).
func FindNetwork(networks []verda.PrivateNetwork, idOrName string) *verda.PrivateNetwork {
	for i := range networks {
		if networks[i].ID == idOrName {
			return &networks[i]
		}
	}
	for i := range networks {
		if networks[i].Name == idOrName {
			return &networks[i]
		}
	}
	return nil
}

// FindSubnet matches by ID first, then by name (names are unique in a network).
func FindSubnet(subnets []verda.Subnet, idOrName string) *verda.Subnet {
	for i := range subnets {
		if subnets[i].ID == idOrName {
			return &subnets[i]
		}
	}
	for i := range subnets {
		if subnets[i].Name == idOrName {
			return &subnets[i]
		}
	}
	return nil
}

// ResolveSubnet finds a subnet by ID or name. With no network it searches every
// network: subnet IDs are unique everywhere, but a name may repeat across networks.
func ResolveSubnet(ctx context.Context, client *verda.Client, networkIDOrName, idOrName string) (*verda.PrivateNetwork, *verda.Subnet, error) {
	if networkIDOrName != "" {
		network, err := ResolveNetwork(ctx, client, networkIDOrName)
		if err != nil {
			return nil, nil, err
		}
		subnets, err := client.PrivateNetworks.GetSubnets(ctx, network.ID)
		if err != nil {
			return nil, nil, err
		}
		if s := FindSubnet(subnets, idOrName); s != nil {
			return network, s, nil
		}
		return nil, nil, NewNotFoundError("subnet", idOrName)
	}

	networks, err := client.PrivateNetworks.Get(ctx)
	if err != nil {
		return nil, nil, err
	}
	type match struct {
		network *verda.PrivateNetwork
		subnet  verda.Subnet
	}
	var byName []match
	for i := range networks {
		subnets, err := client.PrivateNetworks.GetSubnets(ctx, networks[i].ID)
		if err != nil {
			return nil, nil, err
		}
		for j := range subnets {
			if subnets[j].ID == idOrName {
				return &networks[i], &subnets[j], nil
			}
			if subnets[j].Name == idOrName {
				byName = append(byName, match{&networks[i], subnets[j]})
			}
		}
	}
	switch len(byName) {
	case 0:
		return nil, nil, NewNotFoundError("subnet", idOrName)
	case 1:
		return byName[0].network, &byName[0].subnet, nil
	}
	names := make([]string, len(byName))
	for i := range byName {
		names[i] = byName[i].network.Name
	}
	return nil, nil, &AgentError{
		Code:     "VALIDATION_ERROR",
		Message:  fmt.Sprintf("%d subnets named %q in different networks (%s); pass --network", len(byName), idOrName, strings.Join(names, ", ")),
		Details:  map[string]any{"field": "network", "networks": names},
		ExitCode: ExitBadArgs,
	}
}

// ListInstancesOn lists the instances attached to a network, or to one subnet if subnetID is set.
// Workaround until the SDK wraps GET /private-networks/{id}/workloads.
func ListInstancesOn(ctx context.Context, client *verda.Client, networkID, subnetID string) ([]verda.Instance, error) {
	all, err := client.Instances.List(ctx, nil)
	if err != nil {
		return nil, err
	}
	return FilterInstancesOn(all, networkID, subnetID), nil
}

// FilterInstancesOn keeps the instances on networkID (and subnetID when set).
func FilterInstancesOn(instances []verda.Instance, networkID, subnetID string) []verda.Instance {
	out := make([]verda.Instance, 0, len(instances))
	for i := range instances {
		inst := &instances[i]
		if inst.PrivateNetworkID == nil || *inst.PrivateNetworkID != networkID {
			continue
		}
		if subnetID != "" && (inst.SubnetID == nil || *inst.SubnetID != subnetID) {
			continue
		}
		out = append(out, *inst)
	}
	return out
}

// CodeResourceInUse: a delete was refused because something still depends on the resource.
const CodeResourceInUse = "RESOURCE_IN_USE"

// NewResourceInUseError refuses a delete; blockers lists what must go first (details.blockers).
func NewResourceInUseError(resource, id, msg string, blockers any) *AgentError {
	return &AgentError{
		Code:     CodeResourceInUse,
		Message:  msg,
		Details:  map[string]any{"resource": resource, "id": id, "blockers": blockers},
		ExitCode: ExitGeneral,
	}
}
