package buffer

// Copyright (C) 2017-2018  DawnDIY<dawndiy.dev@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

import (
	"sync/atomic"
)

type LeakyBuf struct {
	totalNum   int64
	extraNum   int64
	maxFreeLen int64
	getTimes   int64
	putTimes   int64
	systemType string
	isMemHigh  bool
}

func NewLeakyBuf(n int64, platform string, isMemHigh bool) *LeakyBuf {
	return &LeakyBuf{
		totalNum:   0,
		extraNum:   0,
		maxFreeLen: n,
		systemType: platform,
		isMemHigh:  isMemHigh,
	}
}

func (l *LeakyBuf) Get(cap int) (b *View) {
	totalNum := atomic.LoadInt64(&l.totalNum)
	if totalNum < l.maxFreeLen {
		b = NewView(cap)
		atomic.AddInt64(&l.totalNum, 1)
	} else {
		// IOS-LOGIC
		// if l.systemType == "ios" {
		// 	if !l.isMemHigh {
		// 		if GetChunkPoolUsingBytes() > 150*1024 {
		// 			time.Sleep(30 * time.Millisecond)
		// 		}
		// 	} else {
		// 		if GetChunkPoolUsingBytes() > 10*1024*1024 {
		// 			// time.Sleep(50 * time.Millisecond)
		// 		}
		// 	}
		// }

		b = NewView(cap)
		atomic.AddInt64(&l.extraNum, 1)
	}

	b.leakyBufHandler = l

	atomic.AddInt64(&l.getTimes, 1)
	return
}

func (l *LeakyBuf) put(b *View) {
	extraNum := atomic.LoadInt64(&l.extraNum)

	if extraNum > 0 {
		atomic.AddInt64(&l.extraNum, -1)
	} else {
		totalNum := atomic.LoadInt64(&l.totalNum)
		if totalNum > 0 {
			atomic.AddInt64(&l.totalNum, -1)
		}
	}

	atomic.AddInt64(&l.putTimes, 1)

	return
}

func (l *LeakyBuf) Len() int {
	totalNum := atomic.LoadInt64(&l.totalNum)

	return int(totalNum)
}

func (l *LeakyBuf) Times() int64 {
	getTimes := atomic.LoadInt64(&l.getTimes)
	putTimes := atomic.LoadInt64(&l.putTimes)
	return getTimes - putTimes
}

func (l *LeakyBuf) TotalNums() int64 {
	return atomic.LoadInt64(&l.totalNum)
}

func (l *LeakyBuf) GetTimes() int64 {
	return atomic.LoadInt64(&l.getTimes)
}

func (l *LeakyBuf) PutTimes() int64 {
	return atomic.LoadInt64(&l.putTimes)
}
