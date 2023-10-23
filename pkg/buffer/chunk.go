// Copyright 2022 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package buffer

import (
	"fmt"
	"time"

	"gvisor.dev/gvisor/pkg/bits"
	"gvisor.dev/gvisor/pkg/sync"
)

const (
	// This is log2(baseChunkSize). This number is used to calculate which pool
	// to use for a payload size by right shifting the payload size by this
	// number and passing the result to MostSignificantOne64.
	baseChunkSizeLog2 = 6

	// This is the size of the buffers in the first pool. Each subsquent pool
	// creates payloads 2^(pool index) times larger than the first pool's
	// payloads.
	baseChunkSize = 1 << baseChunkSizeLog2 // 64

	// MaxChunkSize is largest payload size that we pool. Payloads larger than
	// this will be allocated from the heap and garbage collected as normal.
	MaxChunkSize = baseChunkSize << (numPools - 1) // 64k

	// The number of chunk pools we have for use.
	numPools = 11
)

// chunkPools is a collection of pools for payloads of different sizes. The
// size of the payloads doubles in each successive pool.
var chunkPools [numPools]sync.Pool

var (
	debugSupport     bool = false
	debugMutex       sync.Mutex
	debugUsingMap    map[int]int = make(map[int]int)
	debugAllocMap    map[int]int = make(map[int]int)
	debugRealSizeMap map[int]int = make(map[int]int)
)

func InternalStartDebug() {
	debugSupport = true

	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			debugMutex.Lock()
			fmt.Printf("bufferv2 debugUsingMap: %+v\n", debugUsingMap)
			fmt.Printf("bufferv2 debugAllocMap: %+v\n", debugAllocMap)
			fmt.Printf("bufferv2 debugRealSizeMap: %+v\n", debugRealSizeMap)
			debugMutex.Unlock()
		}
	}()
}

func init() {
	for i := 0; i < numPools; i++ {
		chunkSize := baseChunkSize * (1 << i)
		chunkPools[i].New = func() any {
			return &chunk{
				data: make([]byte, chunkSize),
			}
		}
	}
}

// Precondition: 0 <= size <= maxChunkSize
func getChunkPool(size int) (*sync.Pool, int) {
	idx := 0
	if size > baseChunkSize {
		idx = bits.MostSignificantOne64(uint64(size) >> baseChunkSizeLog2)
		if size > 1<<(idx+baseChunkSizeLog2) {
			idx++
		}
	}
	if idx >= numPools {
		panic(fmt.Sprintf("pool for chunk size %d does not exist", size))
	}
	return &chunkPools[idx], 1 << (idx + baseChunkSizeLog2)
}

// Chunk represents a slice of pooled memory.
//
// +stateify savable
type chunk struct {
	chunkRefs
	data []byte
}

func newChunk(size int) *chunk {
	var c *chunk
	if size > MaxChunkSize {
		c = &chunk{
			data: make([]byte, size),
		}

		if debugSupport {
			debugMutex.Lock()
			if val, ok := debugUsingMap[size]; ok {
				debugUsingMap[size] = val + 1
			} else {
				debugUsingMap[size] = 1
			}

			if val, ok := debugAllocMap[size]; ok {
				debugAllocMap[size] = val + 1
			} else {
				debugAllocMap[size] = 1
			}

			if val, ok := debugRealSizeMap[size]; ok {
				debugRealSizeMap[size] = val + 1
			} else {
				debugRealSizeMap[size] = 1
			}

			debugMutex.Unlock()
		}
	} else {
		pool, allcoSize := getChunkPool(size)

		if debugSupport {
			debugMutex.Lock()
			if val, ok := debugUsingMap[allcoSize]; ok {
				debugUsingMap[allcoSize] = val + 1
			} else {
				debugUsingMap[allcoSize] = 1
			}

			if val, ok := debugAllocMap[allcoSize]; ok {
				debugAllocMap[allcoSize] = val + 1
			} else {
				debugAllocMap[allcoSize] = 1
			}

			if val, ok := debugRealSizeMap[size]; ok {
				debugRealSizeMap[size] = val + 1
			} else {
				debugRealSizeMap[size] = 1
			}
			debugMutex.Unlock()
		}

		c = pool.Get().(*chunk)
		for i := range c.data {
			c.data[i] = 0
		}
	}
	c.InitRefs()
	return c
}

func (c *chunk) destroy() {
	if len(c.data) > MaxChunkSize {
		if debugSupport {
			debugMutex.Lock()
			if val, ok := debugUsingMap[len(c.data)]; ok {
				debugUsingMap[len(c.data)] = val - 1
			} else {
				debugUsingMap[len(c.data)] = 0
			}
			debugMutex.Unlock()
		}

		c.data = nil
		return
	}
	pool, allcoSize := getChunkPool(len(c.data))

	if debugSupport {
		debugMutex.Lock()
		if val, ok := debugUsingMap[allcoSize]; ok {
			debugUsingMap[allcoSize] = val - 1
		} else {
			debugUsingMap[allcoSize] = 0
		}
		debugMutex.Unlock()
	}

	pool.Put(c)
}

func (c *chunk) DecRef() {
	c.chunkRefs.DecRef(c.destroy)
}

func (c *chunk) Clone() *chunk {
	cpy := newChunk(len(c.data))
	copy(cpy.data, c.data)
	return cpy
}
