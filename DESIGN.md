# Minio Design

## Introduction

This document describes the high level software architecture of the Minio Object Storage server.

It is intended for developers who want to [contribute](https://github.com/minio/minio/blob/master/CONTRIBUTING.md) to the development of Minio as well as people generally interested  in the design of Minio.

At its heart Minio is essentially a web server serving large binary blobs over HTTP that are either stored on disk (for put operations) or read from disk (for get operations).

## Code organization

<<describe code organization/naming of source code files>>

## Object API Handlers

The type `objectAPIHandlers` provides the core HTTP handlers for the S3 API. It implements amongst others the following functionality: 

- object-related functionality such as `PutObjectHandler`, `GetObjectHandler`, and `HeadObjectHandler`
- bucket-related functionality such as `PutBucketHandler`, `HeadBucketHandler`, `DeleteBucketHandler`
- listing-related functionality such as `ListBucketsHandler`, `ListObjectsV1/V2Handler`, `ListObjectPartsHandler`
- multipart-upload related functionality such as `NewMultipartUploadHandler`, `CompleteMultipartUploadHandler`, and `AbortMultipartUploadHandler`


The `ObjectAPI` field provides access to the `ObjectLayer` interface that is responsible for the actual persistence of objects based on the mode of operation of Minio.

## Object Handlers

The interface [`ObjectLayer`](https://github.com/minio/minio/blob/master/cmd/object-api-interface.go) is responsible for the actual persistence of objects and is implemented by

- [`fsObjects`](https://github.com/minio/minio/blob/master/cmd/fs-v1.go) 
- [`xlObjects`](https://github.com/minio/minio/blob/master/cmd/xl-v1.go)
- [`GatewayLayer`](https://github.com/minio/minio/blob/master/cmd/gateway-router.go) (see further down below)

The functionality of the `ObjectLayer` interface can be divided into the following categories:
- bucket operations: `MakeBucketWithLocation()`, `GetBucketInfo()`, `ListBuckets()`, `DeleteBucket()`, and `ListObjects()`.
- object operations: `PutObject()`, `GetObject()`, `CopyObject()`, `DeleteObject()`, and `GetObjectInfo()`.
- multipart operations: `NewMultipartUpload()`, `PutObjectPart()`, `CompleteMultipartUpload()`, `ListMultipartUploads()`, and others.
- storage operations: `StorageInfo()` and `Shutdown()`.
- healing operations: `HealObject()`, `ListObjectsHeal()`, `HealBucket()`, and others.

### `fsObjects`

`fsObjects` is responsible for the implementation of the File System mode of operation of Minio. In this mode all contents that os stored under a single directory path are represented as an object store. A new `fsObjects` is created via `newFSObjectLayer()` taking a single directory path as argument.

Buckets are top-level directories and new objects that are uploaded are stored as regular files under their respective path with which they are uploaded. Listing operations will list the local directory structure and return the results in batches. Multipart uploads construct the individual parts into a single concatenated file and move it in place once completed.

Get and put operations either read or write to the correspondingly underlying file and deal with the contents according to the S3 API protocol.

As `fsObjects` does not store any error correction information, all healing operations are dummy functions.

### `xlObjects`

`xlObjects` is responsible for the implementation of the Erasure Coding mode of operation of Minio. In this mode objects are written across either multiple disks or multiple servers with additional error correction information that protects against failure of one or more disks or servers.

`xlObjects` has a field named `storageDisks` which is a slice (array) of `StorageAPI` objects. Each `StorageAPI` object either handles storage on a local disk or storage via the network on a remote endpoint. It is created via `newXLObjects()` which requires such a slice of `StorageAPI` objects to be passed in.

Information regarding the data and parity block configuration is also stored by `xlObjects` along side information that keeps track of read and write quorums.

#### Erasure coding 

When doing basic file operations such a storing an object, it is split up into a number of data chunks. Parity chunks are computed (encoded) based on the data chunks and the data and parity blocks are then randomly distributed across the available `StorageAPI` objects.

This configuration allows any combination of up to the number of parity chucks of either disks or servers to become inaccessible or damaged while still being able to reconstruct the contents of the original objects (and thus sent it back for Get operation). This is called the _read quorum_ that needs to be available for normal operation.

There is also a _write quorum_ which is one instance more than the read quorum, meaning that at the very minimum Minio can withstand failure of either a single disk or server (in practice it is almost always many more).
 
 In case the read or write quorum is not available for a specific object, appropriate error messages are returned. This means that, for instance, in case of not meeting the write quorum the client knows that an object has not been committed to disk.
 
#### Bitrot detection

For each chunk that is written to disk, Minio will compute a hash value that is stored along side the data on disk. Upon re-reading the data back off the disk (which may be many years down the line), the hash will be recomputed and, in case of any differences to the original hash value, the chunk will be discarded.
 
 When a chunk is discarded, simple the next chunk in the `storageDisks` slice will be used instead meaning that, due to nature of the redundancy of Erasure Coding, no data loss has occured and the object can be returned in its original form (in virtually all cases).

## Storage API

The interface [`StorageAPI`](https://github.com/minio/minio/blob/master/cmd/storage-interface.go) is used by `xlObjects` and implemented by

- [`posix`](https://github.com/minio/minio/blob/master/cmd/posix.go)
- [`networkStorage`](https://github.com/minio/minio/blob/master/cmd/storage-rpc-client.go)
- [`retryStorage`](https://github.com/minio/minio/blob/master/cmd/retry-storage.go)

### Local disk

`posix` implements the `StorageAPI` interface for a single local disk.

__TODO__: describe `posix`

### Network storage

`networkStorage` implements the `StorageAPI` interface for a specific remote endpoint.

__TODO__: describe `networkStorage`

## Gateway

The interface [`GatewayLayer`](https://github.com/minio/minio/blob/master/cmd/gateway-router.go) extends the `ObjectLayer` interface and is implemented by

- [`azureObjects`](https://github.com/minio/minio/blob/master/cmd/gateway-azure.go)

- [`s3Objects`](https://github.com/minio/minio/blob/master/cmd/gateway-s3.go)

- `gcsObjects`

As compared to the `fsObjects` and `xlObjects` modes of operation of Minio whereby objects are written to local disk, the gateway mode of operation of Minio relies on a back-end store that can vary from Amazon S3 to Microsoft Azure or to Google Cloud Storage (with potentially others to follow in the future).

__TODO__: describe `GatewayLayer`

## Server mux/routes

__TODO__: describe `ServerMux` and more

## Locking 

__TODO__: describe `nsLockMap`,  `lockServer`, `minio/dsync`

## Admin API Handlers

Minio server has a companion tool called [`mc`](https://github.com/minio/mc) (minio client for short) that allows some administrative tasks to be accompished. The interface [`adminCmdRunner`](https://github.com/minio/minio/blob/master/cmd/admin-rpc-client.go) plays a role in this. 

__TODO__: `adminAPIHandlers`

__TODO__: `localAdminClient`

__TODO__: `remoteAdminClient`

## Browser API Handlers

Minio offers an integrated web server that makes it easy to view the contents that it stores using any internet browser (as long as there is a valid HTTP connection possible of course). The type `webAPIHandlers` is the main handler for the Web API:
- `ListObjects`, `ListBuckets`
- `MakeBucket`
- `PresignedGet`

__TODO__: describe 

## Server startup & initialization

__TODO__: describe server start/initialisation of main components

## Layout on disk

Minio has been designed with simplicitly in mind. This permeates not only to 'operational' simplicity (not needing config files, external dependencies, setup scripts, etc. in order to run Minio), but also to keeping the actual storage of objects as plain and simple as possible. Many years down the line people, without hypothetical access to (source code of) Minio itself, should be able to figure out how to reconstruct the data themselves.

For the File System/`fsObjects` mode this is relatively trivial as it directly represents the underlying disk. More information can be found [here](https://github.com/minio/minio/tree/master/docs/backend/fs).

For the Erasure Coding/`xlObjects` mode, a simple directory and file structure is used in combination with JSON files that describe the properties (such as hash value and algorithm for bitrot protection) of the chunks that are written. In fact it should be entirely possible to read/reconstruct data outside of Minio server. Full details can be found [here](https://github.com/minio/minio/tree/master/docs/backend/xl).
 
## What did we not discuss

This document has focused on the most important design aspects of Minio but necessarily has not been able to cover 'all' topics. As such below is a non-conclusive list of topics that are not covered here (along with pointers in case there is additional documentation):
- bucket [notifications](https://github.com/minio/minio/tree/master/docs/bucket/notifications) and [policies](https://github.com/minio/minio/tree/master/docs/bucket/policy)
- configuration data as stored in [config.json](https://github.com/minio/minio/tree/master/docs/config)
- [shared](https://github.com/minio/minio/blob/master/docs/shared-backend/DESIGN.md) back-end mode 
- healing 

## TODO

- add one or more diagrams 
