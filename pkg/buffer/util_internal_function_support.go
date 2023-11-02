//go:build internal
// +build internal

package buffer

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"gvisor.dev/gvisor/pkg/sync"
)

var (
	debugChunkSupport bool = true
	debugViewSupport  bool = true
)

type realSizeNode struct {
	RealSize int `json:"realSize"`
	Counter  int `json:"counter"`
}

var realSizeNodePool = sync.Pool{
	New: func() any {
		return &realSizeNode{}
	},
}

func InternalStartDebugChunk() {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			debugMutex.Lock()

			realSizeMapList := []*realSizeNode{}
			for key, value := range debugRealSizeMap {
				if value != 0 {
					var node *realSizeNode = realSizeNodePool.Get().(*realSizeNode)
					node.RealSize = key
					node.Counter = value

					realSizeMapList = append(realSizeMapList, node)
				} else {
					var node *realSizeNode = realSizeNodePool.Get().(*realSizeNode)
					node.RealSize = key
					node.Counter = value

					realSizeMapList = append(realSizeMapList, node)
				}
			}
			sort.Slice(realSizeMapList, func(i, j int) bool {
				return realSizeMapList[i].Counter*realSizeMapList[i].RealSize > realSizeMapList[j].Counter*realSizeMapList[j].RealSize
			})

			data, _ := json.Marshal(realSizeMapList[:20])

			currentUsingBytes := atomic.LoadInt64(&usingBytes)

			currentMaxUsingFuzzyBytes := atomic.LoadInt64(&maxUsingFuzzyBytes)

			fmt.Printf("bufferv2 debugUsingMap: %+v\nbufferv2 debugUsingMaxMap: %+v\nbufferv2 debugAllocMap: %+v\nbufferv2 debugRealSizeMap: %s\nbufferv2 currentUsingBytes: %d\nbufferv2 currentMaxUsingFuzzyBytes: %d\n",
				debugUsingMap, debugUsingMaxMap, debugAllocMap, data, currentUsingBytes, currentMaxUsingFuzzyBytes)

			for _, node := range realSizeMapList {
				realSizeNodePool.Put(node)
			}

			debugMutex.Unlock()
		}
	}()
}

func InternalStartDebugView() {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for range ticker.C {
			debugViewMutex.Lock()
			fmt.Printf("bufferv2 debugViewMap: %+v\n", debugViewMap)
			debugViewMutex.Unlock()
		}
	}()
}
