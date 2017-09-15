/*
 * Minio Cloud Storage, (C) 2016, 2017 Minio, Inc.
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
	"math/rand"
	"path"
)

// getSlotsForBucket - will convert incoming bucket name to
// corresponding slots for the bucket on the erasure code backend.
func (xl xlObjects) getSlotsForBucket(bucket string) (bucketSlots []BucketSlot, e error) {
	// Verify if bucket is valid.
	if !IsValidBucketName(bucket) {
		return nil, traceError(BucketNameInvalid{Bucket: bucket})
	}

	for _, bucketInfo := range xl.bucketSlots {
		for _, disk := range bucketInfo.storageDisks {
			if _, err := disk.StatVol(bucket); err == nil {
				// If corresponding directory exists, add to list
				bucketSlots = append(bucketSlots, bucketInfo)
				break
			}
		}
	}
	return bucketSlots, nil
}

// This function does the following check, suppose
// object is "a/b/c/d", stat makes sure that objects ""a/b/c""
// "a/b" and "a" do not exist.
func (xl xlObjects) parentDirIsObject(bucket, parent string) bool {
	var isParentDirObject func(string) bool
	isParentDirObject = func(p string) bool {
		if p == "." {
			return false
		}
		if xl.isObject(bucket, p) {
			// If there is already a file at prefix "p" return error.
			return true
		}
		// Check if there is a file as one of the parent paths.
		return isParentDirObject(path.Dir(p))
	}
	return isParentDirObject(parent)
}

// isObject - returns `true` if the prefix is an object i.e if
// `xl.json` exists at the leaf, false otherwise.
func (xl xlObjects) isObject(bucket, prefix string) (ok bool) {
	_, _, ok = xl.getObjectSlot(bucket, prefix)
	return ok
}

// getObjectSlot - returns slot if the prefix is an object
// i.e if  `xl.json` exists at the leaf in one of the bucket slots.
func (xl xlObjects) getObjectSlot(bucket, prefix string) (bucketSlots []BucketSlot, bucketSlot BucketSlot, ok bool) {
	var err error
	bucketSlots, err = xl.getSlotsForBucket(bucket)
	if err == nil {
		for _, slot := range bucketSlots {
			if slot.isObject(bucket, prefix) {
				bucketSlot, ok = slot, true
				break
			}
		}
	}
	return
}

// Calculate the space occupied by an object in a single disk
func (xl xlObjects) sizeOnDisk(fileSize int64, blockSize int64, dataBlocks int) int64 {
	numBlocks := fileSize / blockSize
	chunkSize := getChunkSize(blockSize, dataBlocks)
	sizeInDisk := numBlocks * chunkSize
	remaining := fileSize % blockSize
	if remaining > 0 {
		sizeInDisk += getChunkSize(remaining, dataBlocks)
	}

	return sizeInDisk
}

// getReadableSlot - search if object exists in one of the bucket slots
func (xl *xlObjects) getReadableSlot(bucket, object string) (BucketSlot, error) {
	_, bucketSlot, exists := xl.getObjectSlot(bucket, object)
	if exists {
		return bucketSlot, nil
	}
	return BucketSlot{}, traceError(errFileNotFound)
}

// getWritableSlot - search if object already exists in one
// of the bucket slots and return the slot if so. Otherwise assign the
// object to a new slot
func (xl *xlObjects) getWritableSlot(bucket, object string) (BucketSlot, error) {
	bucketSlots, bucketSlot, exists := xl.getObjectSlot(bucket, object)
	if exists {
		return bucketSlot, nil
	}

	// For now randomly pick the slot in which to put the new object
	slot := rand.Intn(len(bucketSlots))
	return bucketSlots[slot], nil
}
