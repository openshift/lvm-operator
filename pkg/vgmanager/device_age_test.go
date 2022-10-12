package vgmanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type fakeClock struct {
	ftime time.Time
}

func (f *fakeClock) getCurrentTime() time.Time {
	return f.ftime
}

func Test_isOlderThan(t *testing.T) {
	start := time.Now()
	a := &ageMap{
		clock: &fakeClock{ftime: start},
		ageMap: map[string]time.Time{
			"/dev/sdb": time.Now(),
			"/dev/sdc": start.Add(-(time.Second + deviceMinAge)),
			"/dev/sdd": start.Add(-(deviceMinAge / 2)),
		},
	}

	testcases := []struct {
		label    string
		device   string
		expected bool
	}{
		{label: "device is old enough", device: "/dev/sdc", expected: true},
		{label: "device not old enough", device: "/dev/sdb", expected: false},
		{label: "device not old enough", device: "/dev/sdd", expected: false},
		{label: "device not found", device: "/dev/sde", expected: false},
	}

	for _, tc := range testcases {

		result := a.isOlderThan(tc.device)
		assert.Equal(t, tc.expected, result)
	}
}

func Test_storeAgeMap(t *testing.T) {
	myFakeClock := &fakeClock{ftime: time.Now()}
	a := &ageMap{
		clock: myFakeClock,
		ageMap: map[string]time.Time{
			"/dev/sdb": time.Now(),
		},
	}
	_, found := a.ageMap["/dev/nvme0"]
	assert.False(t, found)
	a.storeDeviceAge("/dev/nvme0")
	_, found = a.ageMap["/dev/nvme0"]
	assert.True(t, found)
}
