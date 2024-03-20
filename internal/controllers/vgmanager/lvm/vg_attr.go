package lvm

import (
	"fmt"
)

type Resizeable rune

const (
	ResizeableResizable Resizeable = 'z'
	ResizeableNone      Resizeable = '-'
)

type Exported rune

const (
	ExportedExported Exported = 'x'
	ExportedNone     Exported = '-'
)

type ClusteredOrShared rune

const (
	Clustered ClusteredOrShared = 'c'
	Shared    ClusteredOrShared = 's'
)

// VgAttr has mapped vg_attr information, see 'man vgs' vg_attr bits.
// It is a complete parsing of the entire attribute byte flags that is attached to each VG.
// This is useful when attaching logic to the state of an VG as the state of an VG can be determined
// from the Attribute.
type VgAttr struct {
	Permissions
	Resizeable
	Exported
	Partial
	AllocationPolicy
	ClusteredOrShared
}

func ParsedVgAttr(raw string) (VgAttr, error) {
	if len(raw) != 6 {
		return VgAttr{}, fmt.Errorf("%s is an invalid length vg_attr", raw)
	}
	return VgAttr{
		Permissions(raw[0]),
		Resizeable(raw[1]),
		Exported(raw[2]),
		Partial(raw[3]),
		AllocationPolicy(raw[4]),
		ClusteredOrShared(raw[5]),
	}, nil
}

func (l VgAttr) String() string {
	return fmt.Sprintf(
		"%c%c%c%c%c%c",
		l.Permissions,
		l.Resizeable,
		l.Exported,
		l.Partial,
		l.AllocationPolicy,
		l.ClusteredOrShared,
	)
}
