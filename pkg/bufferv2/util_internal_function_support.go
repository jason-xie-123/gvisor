//go:build internal
// +build internal

package buffer

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync/atomic"
	"time"
)

var (
	debugChunkSupport bool = true
	debugViewSupport  bool = true
)

func InternalStartDebugChunk() {
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
