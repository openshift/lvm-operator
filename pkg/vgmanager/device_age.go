/*
Copyright 2021 Red Hat Openshift Data Foundation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vgmanager

import (
	"sync"
	"time"
)

var (
	// deviceMinAge is the minimum age for a device to be considered safe to claim
	// otherwise, it could be a device that some other entity has attached and we have not claimed.
	deviceMinAge = time.Second * 30
)

// timeInterface exists so as it can be patched for testing purpose
type timeInterface interface {
	getCurrentTime() time.Time
}

type wallTime struct{}

func (t *wallTime) getCurrentTime() time.Time {
	return time.Now()
}

type ageMap struct {
	ageMap map[string]time.Time
	mux    sync.RWMutex
	clock  timeInterface
}

func newAgeMap(clock timeInterface) *ageMap {
	return &ageMap{
		clock:  clock,
		ageMap: map[string]time.Time{},
	}
}

// checks if older than,
// records current time if this is the first observation of key
func (a *ageMap) isOlderThan(key string) bool {
	a.mux.RLock()
	defer a.mux.RUnlock()

	firstObserved, found := a.ageMap[key]
	if !found {
		return false
	}
	return a.clock.getCurrentTime().Sub(firstObserved) > deviceMinAge
}

func (a *ageMap) storeDeviceAge(key string) {
	a.mux.Lock()
	defer a.mux.Unlock()

	_, found := a.ageMap[key]
	// set firstObserved if it doesn't exist
	if !found {
		a.ageMap[key] = a.clock.getCurrentTime()
	}
}
