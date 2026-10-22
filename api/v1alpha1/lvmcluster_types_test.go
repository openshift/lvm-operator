package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDevicePathRegexHelpers(t *testing.T) {
	tests := []struct {
		path    DevicePath
		isRegex bool
		pattern string
	}{
		{path: "/dev/sda", pattern: "/dev/sda"},
		{path: "regex:/dev/sd[a-z]+", isRegex: true, pattern: "/dev/sd[a-z]+"},
		{path: "regex:^/dev/nvme.*$", isRegex: true, pattern: "^/dev/nvme.*$"},
		{path: "regex:", isRegex: true},
	}

	for _, test := range tests {
		t.Run(test.path.Unresolved(), func(t *testing.T) {
			assert.Equal(t, test.isRegex, test.path.IsRegex())
			assert.Equal(t, test.pattern, test.path.Pattern())
		})
	}
}
