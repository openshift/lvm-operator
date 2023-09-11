package vgmanager

import (
	"fmt"
)

type VolumeType rune

const (
	VolumeTypeMirrored                   VolumeType = 'm'
	VolumeTypeMirroredNoInitialSync      VolumeType = 'M'
	VolumeTypeOrigin                     VolumeType = 'o'
	VolumeTypeOriginWithMergingSnapshot  VolumeType = 'O'
	VolumeTypeRAID                       VolumeType = 'r'
	VolumeTypeRAIDNoInitialSync          VolumeType = 'R'
	VolumeTypeSnapshot                   VolumeType = 's'
	VolumeTypeMergingSnapshot            VolumeType = 'S'
	VolumeTypePVMove                     VolumeType = 'p'
	VolumeTypeVirtual                    VolumeType = 'v'
	VolumeTypeMirrorOrRAIDImage          VolumeType = 'i'
	VolumeTypeMirrorOrRAIDImageOutOfSync VolumeType = 'I'
	VolumeTypeMirrorLogDevice            VolumeType = 'l'
	VolumeTypeUnderConversion            VolumeType = 'c'
	VolumeTypeThinVolume                 VolumeType = 'V'
	VolumeTypeThinPool                   VolumeType = 't'
	VolumeTypeThinPoolData               VolumeType = 'T'
	VolumeTypeThinPoolMetadata           VolumeType = 'e'
	VolumeTypeNone                       VolumeType = '-'
)

type Permissions rune

const (
	PermissionsWriteable                             Permissions = 'w'
	PermissionsReadOnly                              Permissions = 'r'
	PermissionsReadOnlyActivationOfNonReadOnlyVolume Permissions = 'R'
	PermissionsNone                                  Permissions = '-'
)

type AllocationPolicy rune

const (
	AllocationPolicyAnywhere         AllocationPolicy = 'a'
	AllocationPolicyAnywhereLocked   AllocationPolicy = 'A'
	AllocationPolicyContiguous       AllocationPolicy = 'c'
	AllocationPolicyContiguousLocked AllocationPolicy = 'C'
	AllocationPolicyInherited        AllocationPolicy = 'i'
	AllocationPolicyInheritedLocked  AllocationPolicy = 'I'
	AllocationPolicyCling            AllocationPolicy = 'l'
	AllocationPolicyClingLocked      AllocationPolicy = 'L'
	AllocationPolicyNormal           AllocationPolicy = 'n'
	AllocationPolicyNormalLocked     AllocationPolicy = 'N'
	AllocationPolicyNone                              = '-'
)

type Minor rune

const (
	MinorTrue  Minor = 'm'
	MinorFalse Minor = '-'
)

type State rune

const (
	StateActive                                State = 'a'
	StateSuspended                                   = 's'
	StateInvalidSnapshot                             = 'I'
	StateSuspendedSnapshot                           = 'S'
	StateSnapshotMergeFailed                         = 'm'
	StateSuspendedSnapshotMergeFailed                = 'M'
	StateMappedDevicePresentWithoutTables            = 'd'
	StateMappedDevicePresentWithInactiveTables       = 'i'
	StateNone                                        = '-'
)

type Open rune

const (
	OpenTrue  Open = 'o'
	OpenFalse Open = '-'
)

type OpenTarget rune

const (
	OpenTargetMirror   = 'm'
	OpenTargetRaid     = 'r'
	OpenTargetSnapshot = 's'
	OpenTargetThin     = 't'
	OpenTargetUnknown  = 'u'
	OpenTargetVirtual  = 'v'
)

type Zero rune

const (
	ZeroTrue  Zero = 'z'
	ZeroFalse Zero = '-'
)

type Partial rune

const (
	PartialTrue  = 'p'
	PartialFalse = '-'
)

// LvAttr has mapped lv_attr information, see https://linux.die.net/man/8/lvs
// It is a complete parsing of the entire attribute byte flags that is attached to each LV.
// This is useful when attaching logic to the state of an LV as the state of an LV can be determined
// from the Attributes, e.g. for determining whether an LV is considered a Thin-Pool or not.
type LvAttr struct {
	VolumeType
	Permissions
	AllocationPolicy
	Minor
	State
	Open
	OpenTarget
	Zero
	Partial
}

func ParsedLvAttr(raw string) (LvAttr, error) {
	if len(raw) != 10 {
		return LvAttr{}, fmt.Errorf("%s is an invalid length lv_attr", raw)
	}
	return LvAttr{
		VolumeType(raw[0]),
		Permissions(raw[1]),
		AllocationPolicy(raw[2]),
		Minor(raw[3]),
		State(raw[4]),
		Open(raw[5]),
		OpenTarget(raw[6]),
		Zero(raw[7]),
		Partial(raw[8]),
	}, nil
}

func (l LvAttr) String() string {
	return fmt.Sprintf(
		"%c%c%c%c%c%c%c%c%c",
		l.VolumeType,
		l.Permissions,
		l.AllocationPolicy,
		l.Minor,
		l.State,
		l.Open,
		l.OpenTarget,
		l.Zero,
		l.Partial,
	)
}
