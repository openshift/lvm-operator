package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDevicePathIsRegex(t *testing.T) {
	assert.True(t, DevicePath("regex:/dev/sd.").IsRegex())
	assert.False(t, DevicePath("/dev/sda").IsRegex())
	assert.False(t, DevicePath("").IsRegex())
	// the prefix only counts at the very start of the entry
	assert.False(t, DevicePath("/dev/disk/by-id/regex:foo").IsRegex())
}

func TestDevicePathPattern(t *testing.T) {
	assert.Equal(t, "^(?:/dev/sd.)$", DevicePath("regex:/dev/sd.").Pattern())
	assert.Equal(t, "", DevicePath("/dev/sda").Pattern())
}

func TestDevicePathCompile(t *testing.T) {
	re, err := DevicePath("regex:/dev/nvme[0-9]n1").Compile()
	assert.NoError(t, err)
	assert.NotNil(t, re)

	_, err = DevicePath("regex:/dev/sd[").Compile()
	assert.Error(t, err)

	_, err = DevicePath("/dev/sda").Compile()
	assert.ErrorContains(t, err, "is not a regex path")
}

func TestDevicePathMatches(t *testing.T) {
	testcases := []struct {
		label   string
		path    DevicePath
		kname   string
		want    bool
		wantErr bool
	}{
		{label: "literal path never matches", path: "/dev/sda", kname: "/dev/sda", want: false},
		{label: "regex matches", path: "regex:/dev/nvme0n[0-9]", kname: "/dev/nvme0n1", want: true},
		{label: "regex does not match", path: "regex:/dev/nvme[0-9]n1", kname: "/dev/sda", want: false},
		{label: "anchored at the start", path: "regex:/dev/sd.", kname: "/dev/mapper/dev/sda", want: false},
		{label: "anchored at the end", path: "regex:/dev/sd.", kname: "/dev/sda1", want: false},
		// without the non-capturing group this would match, because "^/dev/sda|/dev/sdb$"
		// anchors the first branch at the start only
		{label: "alternation anchors every branch", path: "regex:/dev/sda|/dev/sdb", kname: "/dev/sdax", want: false},
		{label: "alternation matches second branch", path: "regex:/dev/sda|/dev/sdb", kname: "/dev/sdb", want: true},
		{label: "empty pattern matches nothing real", path: "regex:", kname: "/dev/sda", want: false},
		{label: "invalid pattern errors", path: "regex:/dev/sd[", kname: "/dev/sda", wantErr: true},
	}
	for _, tc := range testcases {
		t.Run(tc.label, func(t *testing.T) {
			got, err := tc.path.Matches(tc.kname)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
