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

// BucketSlot - Divides all available disks into slots where buckets can be created.
type BucketSlot struct {
	storageDisks []StorageAPI // Collection of initialized backend disks.
	dataBlocks   int          // dataBlocks count calculated for erasure.
	parityBlocks int          // parityBlocks count calculated for erasure.
	readQuorum   int          // readQuorum minimum required disks to read data.
	writeQuorum  int          // writeQuorum minimum required disks to write data.
}

func getBucketSlots(storageDisks []StorageAPI) int {

	return 4
}

func getDisksForBucketSlot(storageDisks []StorageAPI, slot int) []StorageAPI {

	disksForBucket := []StorageAPI{}

	for i, disk := range storageDisks {
		if i % getBucketSlots(storageDisks) == slot {
			disksForBucket = append(disksForBucket, disk)
		}
	}

	return disksForBucket
}

// getSlotsForBucket - will convert incoming bucket name to
// corresponding slots for the bucket on the erasure code backend.
func (xl xlObjects) getSlotsForBucket(bucket string) ([]BucketSlot, error) {
	// Verify if bucket is valid.
	if !IsValidBucketName(bucket) {
		return nil, traceError(BucketNameInvalid{Bucket: bucket})
	}

	bucketSlots := []BucketSlot{}
	for _, bucketInfo := range xl.bucketSlots {

		for _, disk := range bucketInfo.storageDisks {

			_, err := disk.StatVol(bucket)
			if err == nil {
				// If corresponding directory exists, add to list
				bucketSlots = append(bucketSlots, bucketInfo)
				break
			}

		}
	}
	return bucketSlots, nil
}
