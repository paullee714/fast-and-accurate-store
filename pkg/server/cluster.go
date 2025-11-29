package server

import (
	"fmt"
	"net"
	"strings"
)

// SlotRange maps a contiguous slot range to a target address.
type SlotRange struct {
	Start int
	End   int
	Addr  string
}

// ClusterState holds static slot mapping.
type ClusterState struct {
	slots  int
	ranges []SlotRange
}

func NewClusterState(slots int, self string) *ClusterState {
	return &ClusterState{
		slots:  slots,
		ranges: []SlotRange{{Start: 0, End: slots - 1, Addr: self}},
	}
}

func (c *ClusterState) SetRanges(r []SlotRange) {
	c.ranges = r
}

// Lookup returns the target address for a given slot.
func (c *ClusterState) Lookup(slot int) string {
	for _, r := range c.ranges {
		if slot >= r.Start && slot <= r.End {
			return r.Addr
		}
	}
	// fallback to first
	if len(c.ranges) > 0 {
		return c.ranges[0].Addr
	}
	return ""
}

// SlotsInfo returns a RESP-like text for CLUSTER SLOTS (simple format).
func (c *ClusterState) SlotsInfo() string {
	var b strings.Builder
	for _, r := range c.ranges {
		fmt.Fprintf(&b, "%d-%d %s\n", r.Start, r.End, r.Addr)
	}
	return b.String()
}

// ParseSlotRanges parses a flag like "0-5000:1.1.1.1:6379,5001-16383:2.2.2.2:6379".
func ParseSlotRanges(val string) ([]SlotRange, error) {
	parts := strings.Split(val, ",")
	var out []SlotRange
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		sub := strings.SplitN(p, ":", 2)
		if len(sub) != 2 {
			return nil, fmt.Errorf("invalid slot range: %s", p)
		}
		rangePart := sub[0]
		addr := sub[1]
		rr := strings.SplitN(rangePart, "-", 2)
		if len(rr) != 2 {
			return nil, fmt.Errorf("invalid slot range: %s", p)
		}
		var start, end int
		fmt.Sscanf(rr[0], "%d", &start)
		fmt.Sscanf(rr[1], "%d", &end)
		if start > end {
			return nil, fmt.Errorf("start > end in %s", p)
		}
		if _, err := net.ResolveTCPAddr("tcp", addr); err != nil {
			return nil, fmt.Errorf("invalid addr %s: %v", addr, err)
		}
		out = append(out, SlotRange{Start: start, End: end, Addr: addr})
	}
	return out, nil
}
