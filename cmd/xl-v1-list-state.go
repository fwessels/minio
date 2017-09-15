/*
 * Minio Cloud Storage, (C) 2017 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"sync"
	"time"
)

// listStatePool - pool of listState go routines.
// A listState is added to the pool by Set() and removed either by
// doing a Release() or if the concerned timer goes off.
// listStatePool's purpose is to maintain active listState goroutines in a map so that
// it can be looked up across related list calls.
type listStatePool struct {
	pool    map[listParams][]listState
	timeOut time.Duration
	lock    *sync.Mutex
}

// newListStatePool - initialize new pool for list state management.
func newListStatePool(timeout time.Duration) *listStatePool {
	lPool := &listStatePool{
		pool:    make(map[listParams][]listState),
		timeOut: timeout,
		lock:    &sync.Mutex{},
	}
	return lPool
}

// Set - adds a listState to the listStatePool.
// Also starts a timer goroutine that ends when:
// 1) time.After() expires after t.timeOut seconds.
//    The expiration is needed so that the listState goroutine resources are freed after a timeout
//    if the S3 client does only partial listing of objects.
// 2) Release() signals the timer goroutine to end on endTimerCh.
//    During listing the timer should not timeout and end the listState goroutine, hence the
//    timer goroutine should be ended.
func (l listStatePool) Set(params listParams, indices *[]int, mp *map[int]ListObjectsInfo) {
	l.lock.Lock()
	defer l.lock.Unlock()

	// Should be a buffered channel so that Release() never blocks.
	endTimerCh := make(chan struct{}, 1)
	state := listState{
		indices:    indices,
		mapLOI:     mp,
		endTimerCh: endTimerCh,
	}
	// Append new list state.
	l.pool[params] = append(l.pool[params], state)

	// Timer go-routine which times out after t.timeOut seconds.
	go func(endTimerCh <-chan struct{}) {
		select {
		// Wait until timeOut
		case <-time.After(l.timeOut):
			// Timeout has expired. Just remove the list state from listStatePool.
			l.lock.Lock()
			states, ok := l.pool[params]
			if ok {
				// Look for state, remove it from the states list.
				for i, s := range states {
					if s == state {
						states = append(states[:i], states[i+1:]...)
						break
					}
				}
				if len(states) == 0 {
					// No more entries associated with listParams
					// hence remove map entry.
					delete(l.pool, params)
				} else {
					// There are entries associated with listParams
					// hence save the list in the map.
					l.pool[params] = states
				}
			}
			l.lock.Unlock()
		case <-endTimerCh:
			return
		}
	}(endTimerCh)
}

// Release - selects a listState from the pool based on the input
// listParams, removes it from the pool, and returns the ListObjectsInfo
// map.
// Returns nil if listParams does not have an asccociated listState.
func (l *listStatePool) Release(params listParams) (*[]int, *map[int]ListObjectsInfo) {
	l.lock.Lock()
	defer l.lock.Unlock()
	states, ok := l.pool[params] // Check for a valid array of states.
	if ok {
		if len(states) > 0 {
			// Pop out the first valid entry.
			state := states[0]
			states = states[1:]
			if len(states) > 0 {
				l.pool[params] = states
			} else {
				delete(l.pool, params)
			}
			state.endTimerCh <- struct{}{} // Instruct timer to stop
			return state.indices, state.mapLOI
		}
	}
	// Release return nil if params not found.
	return nil, nil
}
