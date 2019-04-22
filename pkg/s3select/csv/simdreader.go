/*
 * Minio Cloud Storage, (C) 2019 Minio, Inc.
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

package csv

import (
	"fmt"
	"bytes"
	"io"
	"strings"
	"github.com/minio/minio/pkg/s3select/sql"
	"github.com/minio/select-simd"
)

func (r *Reader) GetChunks() (result []*bytes.Buffer) { return }
func (r *Reader) GetColumnNames() []string { return r.columnNames }

// ReaderSimd - Simd accelerated CSV record reader for S3Select.
type ReaderSimd struct {
	args        *ReaderArgs
	readCloser  io.ReadCloser
	columnNames []string
	chunks		[]*bytes.Buffer
}

// Read - reads single record.
func (r *ReaderSimd) Read() (sql.Record, error) {
	return nil, nil
}

// Close - closes underlaying reader.
func (r *ReaderSimd) Close() error {
	return r.readCloser.Close()
}

func (r *ReaderSimd) GetChunks() []*bytes.Buffer {
	return r.chunks
}

func (r *ReaderSimd) GetColumnNames() []string { return r.columnNames }

var chunkMap = make(map[string]*[]*bytes.Buffer)
var headersMap = make(map[string][]string)

func NewReaderSimd(readCloser io.ReadCloser, args *ReaderArgs, cachePath string) (*ReaderSimd, error) {

	if args == nil || args.IsEmpty() {
		panic(fmt.Errorf("empty args passed %v", args))
	}

	var pchunks *[]*bytes.Buffer
	var headers []string
	var ok bool
	if pchunks, ok = chunkMap[cachePath]; !ok {
		//fmt.Println("NewReaderSimd: caching for: ", cachePath)
		var raw bytes.Buffer
		raw.Grow(int(1235920388 * 2))
		raw.ReadFrom(readCloser)

		chunks, _ := selectsimd.AlignChunks(0x20000, raw.Bytes())
		pchunks = &chunks
		chunkMap[cachePath] = pchunks

		if args.FileHeaderInfo != none {
			headerSize := bytes.IndexByte(raw.Bytes(), 0x0a)
			header := strings.TrimSpace(string(raw.Bytes()[:headerSize]))
			headersMap[cachePath] = strings.Split(header, ",")
		}
	}
	headers, _ = headersMap[cachePath]

	r := &ReaderSimd{
		args:       args,
		readCloser: readCloser,
		chunks:     *pchunks,
	}

	if args.FileHeaderInfo == none {
		return r, nil
	}

	if args.FileHeaderInfo == use {
		r.columnNames = headers
	}

	return r, nil
}

