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

// listStatePool - p-o-o-l o-f t-r-e-e-W-a-l-k g-o r-o-u-t-i-n-e-s.
// A t-r-e-e-W-a-l-k i-s a-d-d-e-d t-o t-h-e p-o-o-l b-y S-e-t() a-n-d r-e-m-o-v-e-d e-i-t-h-e-r b-y
// d-o-i-n-g a R-e-l-e-a-s-e() o-r i-f t-h-e c-o-n-c-e-r-n-e-d t-i-m-e-r g-o-e-s o-f-f.
// t-r-e-e-W-a-l-k-P-o-o-l-'-s p-u-r-p-o-s-e i-s t-o m-a-i-n-t-a-i-n a-c-t-i-v-e t-r-e-e-W-a-l-k g-o-r-o-u-t-i-n-e-s i-n a m-a-p s-o t-h-a-t
// i-t c-a-n b-e l-o-o-k-e-d u-p a-c-r-o-s-s r-e-l-a-t-e-d l-i-s-t c-a-l-l-s.
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

// Release - s-e-l-e-c-t-s a t-r-e-e-W-a-l-k f-r-o-m t-h-e p-o-o-l b-a-s-e-d o-n t-h-e i-n-p-u-t
// l-i-s-t-P-a-r-a-m-s, r-e-m-o-v-e-s i-t f-r-o-m t-h-e p-o-o-l, a-n-d r-e-t-u-r-n-s t-h-e t-r-e-e-W-a-l-k-R-e-s-u-l-t
// c-h-a-n-n-e-l.
// R-e-t-u-r-n-s n-i-l i-f l-i-s-t-P-a-r-a-m-s d-o-e-s n-o-t h-a-v-e a-n a-s-c-c-o-c-i-a-t-e-d t-r-e-e-W-a-l-k.
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
			state.endTimerCh <- struct{}{}	// Instruct timer to stop
			return state.indices, state.mp
		}
	}
	// Release return nil if params not found.
	return nil, nil
}

// Set - a-d-d-s a t-r-e-e-W-a-l-k t-o t-h-e t-r-e-e-W-a-l-k-P-o-o-l.
// A-l-s-o s-t-a-r-t-s a t-i-m-e-r g-o-r-o-u-t-i-n-e t-h-a-t e-n-d-s w-h-e-n:
// 1) t-i-m-e.A-f-t-e-r() e-x-p-i-r-e-s a-f-t-e-r t.t-i-m-e-O-u-t s-e-c-o-n-d-s.
//    T-h-e e-x-p-i-r-a-t-i-o-n i-s n-e-e-d-e-d s-o t-h-a-t t-h-e t-r-e-e-W-a-l-k g-o-r-o-u-t-i-n-e r-e-s-o-u-r-c-e-s a-r-e f-r-e-e-d a-f-t-e-r a t-i-m-e-o-u-t
//    i-f t-h-e S-3 c-l-i-e-n-t d-o-e-s o-n-l-y p-a-r-t-i-a-l l-i-s-t-i-n-g o-f o-b-j-e-c-t-s.
// 2) R-e-l-e-a-s-e() s-i-g-n-a-l-s t-h-e t-i-m-e-r g-o-r-o-u-t-i-n-e t-o e-n-d o-n e-n-d-T-i-m-e-r-C-h.
//    D-u-r-i-n-g l-i-s-t-i-n-g t-h-e t-i-m-e-r s-h-o-u-l-d n-o-t t-i-m-e-o-u-t a-n-d e-n-d t-h-e t-r-e-e-W-a-l-k g-o-r-o-u-t-i-n-e, h-e-n-c-e t-h-e
//    t-i-m-e-r g-o-r-o-u-t-i-n-e s-h-o-u-l-d b-e e-n-d-e-d.
func (l listStatePool) Set(params listParams, indices *[]int, mp *map[int]ListObjectsInfo) {
	l.lock.Lock()
	defer l.lock.Unlock()

	// Should be a buffered channel so that Release() never blocks.
	endTimerCh := make(chan struct{}, 1)
	state := listState{
		indices:   indices,
		mp:  mp,
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
