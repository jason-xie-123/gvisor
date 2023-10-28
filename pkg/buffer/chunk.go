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
	"sort"
	"sync/atomic"
	"time"

	json "github.com/bytedance/sonic"
	"gvisor.dev/gvisor/pkg/sync"
)

const (
	// This is log2(baseChunkSize). This number is used to calculate which pool
	// to use for a payload size by right shifting the payload size by this
	// number and passing the result to MostSignificantOne64.
	// baseChunkSizeLog2 = 6

	// This is the size of the buffers in the first pool. Each subsquent pool
	// creates payloads 2^(pool index) times larger than the first pool's
	// payloads.
	// baseChunkSize = 1 << baseChunkSizeLog2 // 64

	// MaxChunkSize is largest payload size that we pool. Payloads larger than
	// this will be allocated from the heap and garbage collected as normal.
	// MaxChunkSize = baseChunkSize << (numPools - 1) // 64k
	MaxChunkSize = 65536

	// The number of chunk pools we have for use.
	numPools = 12
)

// chunkPools is a collection of pools for payloads of different sizes. The
// size of the payloads doubles in each successive pool.
var chunkPools [numPools]sync.Pool

var (
	poolSizes = [numPools]int{64, 128, 256, 512, 1024, 1500, 2048, 4096, 8192, 16384, 32768, 65536}

	usingBytes         int64 = 0
	maxUsingFuzzyBytes int64 = 0
	debugSupport       bool  = false
	debugMutex         sync.Mutex
	debugUsingMap      map[int]int = make(map[int]int)
	debugUsingMaxMap   map[int]int = make(map[int]int)
	debugAllocMap      map[int]int = make(map[int]int)
	debugRealSizeMap   map[int]int = make(map[int]int)
)

type realSizeNode struct {
	RealSize int `json:"realSize"`
	Counter  int `json:"counter"`
}

func InternalStartDebugChunk() {
	debugSupport = true

	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			debugMutex.Lock()
			fmt.Printf("bufferv2 debugUsingMap: %+v\n", debugUsingMap)
			fmt.Printf("bufferv2 debugUsingMaxMap: %+v\n", debugUsingMaxMap)
			fmt.Printf("bufferv2 debugAllocMap: %+v\n", debugAllocMap)

			realSizeMapList := []*realSizeNode{}
			for key, value := range debugRealSizeMap {
				if value != 0 {
					realSizeMapList = append(realSizeMapList, &realSizeNode{
						key, value,
					})
				} else {
					fmt.Println("find zero real size trunk!!!!!!!!!!!!!")
					realSizeMapList = append(realSizeMapList, &realSizeNode{
						key, value,
					})
				}
			}
			sort.Slice(realSizeMapList, func(i, j int) bool {
				return realSizeMapList[i].Counter*realSizeMapList[i].RealSize > realSizeMapList[j].Counter*realSizeMapList[j].RealSize
			})

			if len(realSizeMapList) > 20 {
				realSizeMapList = realSizeMapList[:20]
			}

			data, _ := json.Marshal(realSizeMapList)
			fmt.Printf("bufferv2 debugRealSizeMap: %s\n", data)

			currentUsingBytes := atomic.LoadInt64(&usingBytes)
			fmt.Printf("bufferv2 currentUsingBytes: %d\n", currentUsingBytes)

			currentMaxUsingFuzzyBytes := atomic.LoadInt64(&maxUsingFuzzyBytes)
			fmt.Printf("bufferv2 currentMaxUsingFuzzyBytes: %d\n", currentMaxUsingFuzzyBytes)

			debugMutex.Unlock()
		}
	}()
}

func init() {
	for i := 0; i < numPools; i++ {
		chunkSize := poolSizes[i]
		chunkPools[i].New = func() any {
			return &chunk{
				data: make([]byte, chunkSize),
			}
		}
	}
}

func GetChunkPoolUsingBytes() int64 {
	return atomic.LoadInt64(&usingBytes)
}

// Precondition: 0 <= size <= maxChunkSize
func getChunkPool(size int) (*sync.Pool, int) {
	idx := -1
	for index, poolSize := range poolSizes {
		if size <= poolSize {
			idx = index
			break
		}
	}

	if idx == -1 {
		panic(fmt.Sprintf("pool for chunk size %d does not exist", size))
	}

	return &chunkPools[idx], poolSizes[idx]
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

		atomic.AddInt64(&usingBytes, int64(size))

		currentUsingBytes := atomic.LoadInt64(&usingBytes)
		currentMaxUsingFuzzyBytes := atomic.LoadInt64(&maxUsingFuzzyBytes)
		if currentUsingBytes > currentMaxUsingFuzzyBytes {
			atomic.StoreInt64(&maxUsingFuzzyBytes, currentUsingBytes)
		}

		if debugSupport {
			debugMutex.Lock()
			if val, ok := debugUsingMap[size]; ok {
				debugUsingMap[size] = val + 1
			} else {
				debugUsingMap[size] = 1
			}

			if val, ok := debugUsingMaxMap[size]; ok {
				if val < debugUsingMap[size] {
					debugUsingMaxMap[size] = debugUsingMap[size]
				}
			} else {
				debugUsingMaxMap[size] = debugUsingMap[size]
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

		pool, allocSize := getChunkPool(size)

		if 1024 == allocSize {
			fmt.Println("xxxxxxx")
		}

		atomic.AddInt64(&usingBytes, int64(allocSize))

		currentUsingBytes := atomic.LoadInt64(&usingBytes)
		currentMaxUsingFuzzyBytes := atomic.LoadInt64(&maxUsingFuzzyBytes)
		if currentUsingBytes > currentMaxUsingFuzzyBytes {
			atomic.StoreInt64(&maxUsingFuzzyBytes, currentUsingBytes)
		}

		if debugSupport {
			debugMutex.Lock()
			if val, ok := debugUsingMap[allocSize]; ok {
				debugUsingMap[allocSize] = val + 1
			} else {
				debugUsingMap[allocSize] = 1
			}

			if val, ok := debugUsingMaxMap[allocSize]; ok {
				if val < debugUsingMap[allocSize] {
					debugUsingMaxMap[allocSize] = debugUsingMap[allocSize]
				}
			} else {
				debugUsingMaxMap[allocSize] = debugUsingMap[allocSize]
			}

			if val, ok := debugAllocMap[allocSize]; ok {
				debugAllocMap[allocSize] = val + 1
			} else {
				debugAllocMap[allocSize] = 1
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
		atomic.AddInt64(&usingBytes, -int64(len(c.data)))

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
	pool, allocSize := getChunkPool(len(c.data))

	atomic.AddInt64(&usingBytes, -int64(allocSize))

	if debugSupport {
		debugMutex.Lock()
		if val, ok := debugUsingMap[allocSize]; ok {
			debugUsingMap[allocSize] = val - 1
		} else {
			debugUsingMap[allocSize] = 0
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
