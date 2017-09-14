/*
 * Minio Cloud Storage, (C) 2016 Minio, Inc.
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
	"sort"
	"sync"
)

// list all errors that can be ignore in a bucket operation.
var bucketOpIgnoredErrs = append(baseIgnoredErrs, errDiskAccessDenied)

// list all errors that can be ignored in a bucket metadata operation.
var bucketMetadataOpIgnoredErrs = append(bucketOpIgnoredErrs, errVolumeNotFound)

/// Bucket operations

// MakeBucketWithLocation - make a bucket.
func (xl xlObjects) MakeBucketWithLocation(bucket, location string) error {
	// Verify if bucket is valid.
	if !IsValidBucketName(bucket) {
		return traceError(BucketNameInvalid{Bucket: bucket})
	}

	// Get bucket slot for bucket
	bucketSlots, err := xl.getSlotsForBucket(bucket)
	if err != nil {
		return err
	} else if len(bucketSlots) > 0 {
		// Bucket already exists so exit out early
		return nil
	}

	success := false

	for _, bucketSlot := range xl.bucketSlots {
		err = makeBucketForSlot(&bucketSlot, bucket)
		if err == nil {
			// Successfully created the bucket in at least a single slot, so
			// mark the operation as successful. (Note that requiring the bucket
			// to be created successfully in all bucket slots would mean that a single
			// 'down' slot would prevent the bucket from being created at all.)
			success = true
		}
	}

	if !success {
		// In case we had no success it must be due to all write quorum errors
		return toObjectErr(errXLWriteQuorum, bucket)
	}

	return nil
}

// makeBucketForSlot - makes a bucket for a particular slot.
// TODO: Consider moving to BucketSlot
func makeBucketForSlot(bucketSlot *BucketSlot, bucket string) error {

	// Initialize sync waitgroup.
	var wg = &sync.WaitGroup{}

	// Initialize list of errors.
	var dErrs = make([]error, len(bucketSlot.storageDisks))

	// Make a volume entry on all underlying storage disks.
	for index, disk := range bucketSlot.storageDisks {
		if disk == nil {
			dErrs[index] = traceError(errDiskNotFound)
			continue
		}
		wg.Add(1)
		// Make a volume inside a go-routine.
		go func(index int, disk StorageAPI) {
			defer wg.Done()
			err := disk.MakeVol(bucket)
			if err != nil {
				dErrs[index] = traceError(err)
			}
		}(index, disk)
	}

	// Wait for all make volumes to finish.
	wg.Wait()

	err := reduceWriteQuorumErrs(dErrs, bucketOpIgnoredErrs, bucketSlot.writeQuorum)
	if errorCause(err) == errXLWriteQuorum {
		// Purge successfully created buckets if we don't have writeQuorum.
		undoMakeBucket(bucketSlot.storageDisks, bucket)
	}
	return toObjectErr(err, bucket)
}

func undoDeleteBucket(storageDisks []StorageAPI, bucket string) {
	// Initialize sync waitgroup.
	var wg = &sync.WaitGroup{}
	// Undo previous delete bucket entry on all underlying storage disks.
	for index, disk := range storageDisks {
		if disk == nil {
			continue
		}
		wg.Add(1)
		// Create a bucket inside a go-routine.
		go func(index int, disk StorageAPI) {
			defer wg.Done()
			disk.MakeVol(bucket)
		}(index, disk)
	}

	// Wait for all make volumes to finish.
	wg.Wait()
}

// undo make bucket operation upon quorum failure.
func undoMakeBucket(storageDisks []StorageAPI, bucket string) {
	// Initialize sync waitgroup.
	var wg = &sync.WaitGroup{}
	// Undo previous make bucket entry on all underlying storage disks.
	for index, disk := range storageDisks {
		if disk == nil {
			continue
		}
		wg.Add(1)
		// Delete a bucket inside a go-routine.
		go func(index int, disk StorageAPI) {
			defer wg.Done()
			disk.DeleteVol(bucket)
		}(index, disk)
	}

	// Wait for all delete volumes to finish.
	wg.Wait()
}

// getBucketInfo - returns the BucketInfo from one of the bucket slots.
func (xl xlObjects) getBucketInfo(bucketName string) (bucketInfo BucketInfo, err error) {

	for _, bucketSlot := range xl.bucketSlots {
		bucketInfoSlot, serr := getBucketInfoForSlot(&bucketSlot, bucketName)
		if serr == nil {
			// Check whether uninitialized or whether we found an earlier time
			if len(bucketInfo.Name) == 0 ||
				bucketInfo.Created.UnixNano() > bucketInfoSlot.Created.UnixNano() {
				bucketInfo = bucketInfoSlot
			}
		} else if !isErr(serr, errVolumeNotFound) { // Return error unless volume not found (keep searching)
			return BucketInfo{}, serr
		}
	}
	return bucketInfo, nil
}

// getBucketInfoForSlot - returns the BucketInfo from one of the load balanced disks.
// TODO: Consider moving to BucketSlot
func getBucketInfoForSlot(bucketSlot *BucketSlot, bucketName string) (bucketInfo BucketInfo, err error) {
	var bucketErrs []error
	for _, disk := range bucketSlot.getLoadBalancedDisks() {
		if disk == nil {
			bucketErrs = append(bucketErrs, errDiskNotFound)
			continue
		}
		volInfo, serr := disk.StatVol(bucketName)
		if serr == nil {
			bucketInfo = BucketInfo{
				Name:    volInfo.Name,
				Created: volInfo.Created,
			}
			return bucketInfo, nil
		}
		err = traceError(serr)
		// For any reason disk went offline continue and pick the next one.
		if isErrIgnored(err, bucketMetadataOpIgnoredErrs...) {
			bucketErrs = append(bucketErrs, err)
			continue
		}
		// Any error which cannot be ignored, we return quickly.
		return BucketInfo{}, err
	}
	// If all our errors were ignored, then we try to
	// reduce to one error based on read quorum.
	// `nil` is deliberately passed for ignoredErrs
	// because these errors were already ignored.
	return BucketInfo{}, reduceReadQuorumErrs(bucketErrs, nil, bucketSlot.readQuorum)
}

// GetBucketInfo - returns BucketInfo for a bucket.
func (xl xlObjects) GetBucketInfo(bucket string) (bi BucketInfo, e error) {
	// Verify if bucket is valid.
	if !IsValidBucketName(bucket) {
		return bi, BucketNameInvalid{Bucket: bucket}
	}

	bucketInfo, err := xl.getBucketInfo(bucket)
	if err != nil {
		return bi, toObjectErr(err, bucket)
	}
	return bucketInfo, nil
}

// listBuckets - returns list of all buckets from all slots
// by picking a random disk per slot.
func (xl xlObjects) listBuckets() (bucketsInfo []BucketInfo, err error) {

	bucketMap := make(map[string]BucketInfo)

	for _, bucketSlot := range xl.bucketSlots {

		// Get list of disks for this slot
		disks := bucketSlot.getLoadBalancedDisks()

		// Get the buckets for this slot
		bucketInfos, err := listBucketsForSlot(disks)
		if err != nil {
			return nil, err
		}

		// Iterate over buckets, only adding when new
		for _, bucketInfo := range bucketInfos {
			bi, found := bucketMap[bucketInfo.Name]
			if !found {
				bucketMap[bucketInfo.Name] = bucketInfo
			} else if bi.Created.UnixNano() > bucketInfo.Created.UnixNano() {
				// Take the time of the earliest created directory
				bucketMap[bucketInfo.Name] = bucketInfo
			}
		}
	}

	// Copy map of bucket infos to list
	for _, bucketInfo := range bucketMap {
		bucketsInfo = append(bucketsInfo, bucketInfo)
	}

	return
}

// listBucketsForSlot - returns list of all buckets from a disk picked at random.
func listBucketsForSlot(disks []StorageAPI) (bucketsInfo []BucketInfo, err error) {
	for _, disk := range disks {
		if disk == nil {
			continue
		}
		var volsInfo []VolInfo
		volsInfo, err = disk.ListVols()
		if err == nil {
			// NOTE: The assumption here is that volumes across all disks in
			// readQuorum have consistent view i.e they all have same number
			// of buckets. This is essentially not verified since healing
			// should take care of this.
			var bucketsInfo []BucketInfo
			for _, volInfo := range volsInfo {
				if isReservedOrInvalidBucket(volInfo.Name) {
					continue
				}
				bucketsInfo = append(bucketsInfo, BucketInfo{
					Name:    volInfo.Name,
					Created: volInfo.Created,
				})
			}
			// For buckets info empty, loop once again to check
			// if we have, can happen if disks were down.
			if len(bucketsInfo) == 0 {
				continue
			}
			return bucketsInfo, nil
		}
		err = traceError(err)
		// Ignore any disks not found.
		if isErrIgnored(err, bucketMetadataOpIgnoredErrs...) {
			continue
		}
		break
	}
	return nil, err
}

// ListBuckets - lists all the buckets, sorted by its name.
func (xl xlObjects) ListBuckets() ([]BucketInfo, error) {
	bucketInfos, err := xl.listBuckets()
	if err != nil {
		return nil, toObjectErr(err)
	}
	// Sort by bucket name before returning.
	sort.Sort(byBucketName(bucketInfos))
	return bucketInfos, nil
}

// DeleteBucket - deletes a bucket.
func (xl xlObjects) DeleteBucket(bucket string) error {
	// Verify if bucket is valid.
	if !IsValidBucketName(bucket) {
		return BucketNameInvalid{Bucket: bucket}
	}

	// Get the list of slots that we need to delete
	bucketSlots, err := xl.getSlotsForBucket(bucket)
	if err != nil {
		return err
	}

	allDeleted := true

	for _, bucketSlot := range bucketSlots {
		err = deleteBucketForSlot(&bucketSlot, bucket)
		if err != nil {
			// Mark that at least the delete in one bucket slot failed.
			// This means that the bucket continues to exist in a single
			// slot so we cannot return success (must return error so
			// client may try again later.)
			allDeleted = false
		}
	}

	if !allDeleted {
		return toObjectErr(errXLWriteQuorum, bucket)
	}

	return nil
}

// deleteBucketForSlot - deletes a bucket for a particular slot.
// TODO: Consider moving to BucketSlot
func deleteBucketForSlot(bucketSlot *BucketSlot, bucket string) error {

	// Collect if all disks report volume not found.
	var wg = &sync.WaitGroup{}
	var dErrs = make([]error, len(bucketSlot.storageDisks))

	// Remove a volume entry on all underlying storage disks.
	for index, disk := range bucketSlot.storageDisks {
		if disk == nil {
			dErrs[index] = traceError(errDiskNotFound)
			continue
		}
		wg.Add(1)
		// Delete volume inside a go-routine.
		go func(index int, disk StorageAPI) {
			defer wg.Done()
			// Attempt to delete bucket.
			err := disk.DeleteVol(bucket)
			if err != nil {
				dErrs[index] = traceError(err)
				return
			}
			// Cleanup all previously incomplete multiparts.
			err = cleanupDir(disk, minioMetaMultipartBucket, bucket)
			if err != nil {
				if errorCause(err) == errVolumeNotFound {
					return
				}
				dErrs[index] = err
			}
		}(index, disk)
	}

	// Wait for all the delete vols to finish.
	wg.Wait()

	err := reduceWriteQuorumErrs(dErrs, bucketOpIgnoredErrs, bucketSlot.writeQuorum)
	if errorCause(err) == errXLWriteQuorum {
		undoDeleteBucket(bucketSlot.storageDisks, bucket)
	}
	return toObjectErr(err, bucket)
}
