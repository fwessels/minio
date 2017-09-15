/*
 * Minio Cloud Storage, (C) 2015-2017 Minio, Inc.
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
	"path"
)

// BucketSlot - Divides all available disks into slots where buckets can be created.
type BucketSlot struct {
	storageDisks []StorageAPI // Collection of initialized backend disks.
	// TODO: datablocks & parityBlocks can move up again
	dataBlocks   int // dataBlocks count calculated for erasure.
	parityBlocks int // parityBlocks count calculated for erasure.
	readQuorum   int // readQuorum minimum required disks to read data.
	writeQuorum  int // writeQuorum minimum required disks to write data.
}

func getBucketSlots(storageDisks []StorageAPI) int {

	// TODO: Derive number of bucket slots properly
	return 4
}

func getDisksForBucketSlot(storageDisks []StorageAPI, slot int) []StorageAPI {

	disksForBucket := []StorageAPI{}

	for i, disk := range storageDisks {
		if i%getBucketSlots(storageDisks) == slot {
			disksForBucket = append(disksForBucket, disk)
		}
	}

	return disksForBucket
}

// getLoadBalancedDisks - fetches load balanced (sufficiently randomized) disk slice.
func (bucketSlot *BucketSlot) getLoadBalancedDisks() (disks []StorageAPI) {
	// Based on the random shuffling return back randomized disks.
	for _, i := range hashOrder(UTCNow().String(), len(bucketSlot.storageDisks)) {
		disks = append(disks, bucketSlot.storageDisks[i-1])
	}
	return disks
}

// isObject - returns `true` if the prefix is an object i.e if
// `xl.json` exists at the leaf, false otherwise.
func (bucketSlot *BucketSlot) isObject(bucket, prefix string) bool {
	for _, disk := range bucketSlot.getLoadBalancedDisks() {
		if disk == nil {
			continue
		}
		// Check if 'prefix' is an object on this 'disk', else continue the check the next disk
		_, err := disk.StatFile(bucket, path.Join(prefix, xlMetaJSONFile))
		if err == nil {
			return true
		}
		// Ignore for file not found,  disk not found or faulty disk.
		if isErrIgnored(err, xlTreeWalkIgnoredErrs...) {
			continue
		}
		errorIf(err, "Unable to stat a file %s/%s/%s", bucket, prefix, xlMetaJSONFile)
	} // Exhausted all disks - return false.
	return false
}
