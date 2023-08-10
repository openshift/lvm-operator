package vgmanager

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParsedLvAttr(t *testing.T) {
	type args struct {
		raw string
	}
	tests := []struct {
		name    string
		args    args
		want    LvAttr
		wantErr assert.ErrorAssertionFunc
	}{
		{
			"RAID Config without Initial Sync",
			args{raw: "Rwi-a-r---"},
			LvAttr{
				VolumeType:       VolumeTypeRAIDNoInitialSync,
				Permissions:      PermissionsWriteable,
				AllocationPolicy: AllocationPolicyInherited,
				Minor:            MinorFalse,
				State:            StateActive,
				Open:             OpenFalse,
				OpenTarget:       OpenTargetRaid,
				Zero:             ZeroFalse,
				Partial:          PartialFalse,
			},
			assert.NoError,
		},
		{
			"ThinPool with Zeroing",
			args{raw: "twi-a-tz--"},
			LvAttr{
				VolumeType:       VolumeTypeThinPool,
				Permissions:      PermissionsWriteable,
				AllocationPolicy: AllocationPolicyInherited,
				Minor:            MinorFalse,
				State:            StateActive,
				Open:             OpenFalse,
				OpenTarget:       OpenTargetThin,
				Zero:             ZeroTrue,
				Partial:          PartialFalse,
			},
			assert.NoError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsedLvAttr(tt.args.raw)
			if !tt.wantErr(t, err, fmt.Sprintf("ParsedLvAttr(%v)", tt.args.raw)) {
				return
			}
			assert.Equalf(t, tt.want, got, "ParsedLvAttr(%v)", tt.args.raw)
		})
	}
}
