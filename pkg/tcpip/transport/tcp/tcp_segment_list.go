package tcp

import (
	"fmt"
	"time"

	"gvisor.dev/gvisor/pkg/sync"
)

// ElementMapper provides an identity mapping by default.
//
// This can be replaced to provide a struct that maps elements to linker
// objects, if they are not the same. An ElementMapper is not typically
// required if: Linker is left as is, Element is left as is, or Linker and
// Element are the same type.
type segmentElementMapper struct{}

// linkerFor maps an Element to a Linker.
//
// This default implementation should be inlined.
//
//go:nosplit
func (segmentElementMapper) linkerFor(elem *segment) *segment { return elem }

// List is an intrusive list. Entries can be added to or removed from the list
// in O(1) time and with no additional memory allocations.
//
// The zero value for List is an empty list ready to use.
//
// To iterate over a list (where l is a List):
//
//	for e := l.Front(); e != nil; e = e.Next() {
//		// do something with e.
//	}
//
// +stateify savable
var (
	segmentListMaxCount        int
	segmentListSupportMaxCount int
	segmentListCount           int
	segmentListMutex           sync.Mutex
)

func init() {
	// InternalSetSupportMaxCount(500)
}

type segmentList struct {
	head *segment
	tail *segment
}

func InternalSetSupportMaxCount(count int) {
	segmentListSupportMaxCount = count
}

// Reset resets list l to the empty state.
func (l *segmentList) Reset() {
	l.head = nil
	l.tail = nil
}

// Empty returns true iff the list is empty.
//
//go:nosplit
func (l *segmentList) Empty() bool {
	return l.head == nil
}

// Front returns the first element of list l or nil.
//
//go:nosplit
func (l *segmentList) Front() *segment {
	return l.head
}

// Back returns the last element of list l or nil.
//
//go:nosplit
func (l *segmentList) Back() *segment {
	return l.tail
}

// Len returns the number of elements in the list.
//
// NOTE: This is an O(n) operation.
//
//go:nosplit
func (l *segmentList) Len() (count int) {
	for e := l.Front(); e != nil; e = (segmentElementMapper{}.linkerFor(e)).Next() {
		count++
	}
	return count
}

// PushFront inserts the element e at the front of list l.
//
//go:nosplit
func (l *segmentList) PushFront(e *segment) {
	linker := segmentElementMapper{}.linkerFor(e)
	linker.SetNext(l.head)
	linker.SetPrev(nil)
	if l.head != nil {
		segmentElementMapper{}.linkerFor(l.head).SetPrev(e)
	} else {
		l.tail = e
	}
	l.head = e
}

// PushFrontList inserts list m at the start of list l, emptying m.
//
//go:nosplit
func (l *segmentList) PushFrontList(m *segmentList) {
	if l.head == nil {
		l.head = m.head
		l.tail = m.tail
	} else if m.head != nil {
		segmentElementMapper{}.linkerFor(l.head).SetPrev(m.tail)
		segmentElementMapper{}.linkerFor(m.tail).SetNext(l.head)

		l.head = m.head
	}
	m.head = nil
	m.tail = nil
}

// PushBack inserts the element e at the back of list l.
//
//go:nosplit
func (l *segmentList) PushBack(e *segment) {
	linker := segmentElementMapper{}.linkerFor(e)
	linker.SetNext(nil)
	linker.SetPrev(l.tail)
	if l.tail != nil {
		segmentElementMapper{}.linkerFor(l.tail).SetNext(e)
	} else {
		l.head = e
	}

	l.tail = e

	if segmentListSupportMaxCount > 0 && e.supportMaxCounter {
		segmentListMutex.Lock()
		segmentListCount += 1

		if segmentListCount > segmentListSupportMaxCount {
			time.Sleep(30 * time.Millisecond)
		}
		if segmentListMaxCount < segmentListCount {
			segmentListMaxCount = segmentListCount
		}

		fmt.Printf("segmentList[push] counter: count=%d maxCount=%d\n", segmentListCount, segmentListMaxCount)
		segmentListMutex.Unlock()
	}
}

// PushBackList inserts list m at the end of list l, emptying m.
//
//go:nosplit
func (l *segmentList) PushBackList(m *segmentList) {
	if l.head == nil {
		l.head = m.head
		l.tail = m.tail
	} else if m.head != nil {
		segmentElementMapper{}.linkerFor(l.tail).SetNext(m.head)
		segmentElementMapper{}.linkerFor(m.head).SetPrev(l.tail)

		l.tail = m.tail
	}
	m.head = nil
	m.tail = nil
}

// InsertAfter inserts e after b.
//
//go:nosplit
func (l *segmentList) InsertAfter(b, e *segment) {
	bLinker := segmentElementMapper{}.linkerFor(b)
	eLinker := segmentElementMapper{}.linkerFor(e)

	a := bLinker.Next()

	eLinker.SetNext(a)
	eLinker.SetPrev(b)
	bLinker.SetNext(e)

	if a != nil {
		segmentElementMapper{}.linkerFor(a).SetPrev(e)
	} else {
		l.tail = e
	}
}

// InsertBefore inserts e before a.
//
//go:nosplit
func (l *segmentList) InsertBefore(a, e *segment) {
	aLinker := segmentElementMapper{}.linkerFor(a)
	eLinker := segmentElementMapper{}.linkerFor(e)

	b := aLinker.Prev()
	eLinker.SetNext(a)
	eLinker.SetPrev(b)
	aLinker.SetPrev(e)

	if b != nil {
		segmentElementMapper{}.linkerFor(b).SetNext(e)
	} else {
		l.head = e
	}
}

// Remove removes e from l.
//
//go:nosplit
func (l *segmentList) Remove(e *segment) {
	linker := segmentElementMapper{}.linkerFor(e)
	prev := linker.Prev()
	next := linker.Next()

	if prev != nil {
		segmentElementMapper{}.linkerFor(prev).SetNext(next)
	} else if l.head == e {
		l.head = next
	}

	if next != nil {
		segmentElementMapper{}.linkerFor(next).SetPrev(prev)
	} else if l.tail == e {
		l.tail = prev
	}

	linker.SetNext(nil)
	linker.SetPrev(nil)

	if segmentListSupportMaxCount > 0 && e.supportMaxCounter {
		segmentListMutex.Lock()
		segmentListCount -= 1

		fmt.Printf("segmentList[consume] counter: count=%d maxCount=%d\n", segmentListCount, segmentListMaxCount)
		segmentListMutex.Unlock()
	}
}

// Entry is a default implementation of Linker. Users can add anonymous fields
// of this type to their structs to make them automatically implement the
// methods needed by List.
//
// +stateify savable
type segmentEntry struct {
	next *segment
	prev *segment
}

// Next returns the entry that follows e in the list.
//
//go:nosplit
func (e *segmentEntry) Next() *segment {
	return e.next
}

// Prev returns the entry that precedes e in the list.
//
//go:nosplit
func (e *segmentEntry) Prev() *segment {
	return e.prev
}

// SetNext assigns 'entry' as the entry that follows e in the list.
//
//go:nosplit
func (e *segmentEntry) SetNext(elem *segment) {
	e.next = elem
}

// SetPrev assigns 'entry' as the entry that precedes e in the list.
//
//go:nosplit
func (e *segmentEntry) SetPrev(elem *segment) {
	e.prev = elem
}
