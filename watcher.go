// Copyright 2015 Peter Mattis.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License. See the AUTHORS file
// for names of contributors.

package main

/*
#cgo LDFLAGS: -framework CoreServices
#include <CoreServices/CoreServices.h>
FSEventStreamRef fsevent_create(
	FSEventStreamContext*,
	CFMutableArrayRef,
	FSEventStreamEventId,
	CFTimeInterval,
	FSEventStreamCreateFlags);
*/
import "C"

import (
	"strings"
	"time"
	"unsafe"
)

type CreateFlags uint32

const (
	useCFTypes CreateFlags = 1 << iota
	NoDefer
	WatchRoot
	IgnoreSelf
	FileEvents
)

type EventFlags uint32

const (
	mustScanSubDirs EventFlags = 1 << iota
	userDropped
	kernelDropped
	eventIDWrapped
	historyDone
	rootChanged
	mount
	unmount

	ItemCreated
	ItemRemoved
	ItemInodeMetaMod
	ItemRenamed
	ItemModified
	ItemFinderInfoMod
	ItemChangeOwner
	ItemXattrMod
	ItemIsFile
	ItemIsDir
	ItemIsSymlink
)

func (f EventFlags) String() string {
	var s []string
	if f&ItemCreated != 0 {
		s = append(s, "created")
	}
	if f&ItemRemoved != 0 {
		s = append(s, "removed")
	}
	if f&ItemRenamed != 0 {
		s = append(s, "renamed")
	}
	if f&ItemModified != 0 {
		s = append(s, "modified")
	}
	if f&ItemIsFile != 0 {
		s = append(s, "file")
	}
	if f&ItemIsDir != 0 {
		s = append(s, "dir")
	}
	if f&ItemIsSymlink != 0 {
		s = append(s, "symlink")
	}
	return strings.Join(s, ",")
}

type EventID C.FSEventStreamEventId

// EventID has type UInt64 but this constant is represented as -1
// which is represented by 63 1's in memory
const fseventsNow EventID = (1 << 64) - 1
const fseventsAll EventID = 0

// Event ...
type Event struct {
	ID    EventID
	Path  string
	Flags EventFlags
}

// Stream ...
type Stream struct {
	Chan    chan Event
	cstream C.FSEventStreamRef
	runloop C.CFRunLoopRef
}

// New ...
func NewFSEvents(since EventID, interval time.Duration, flags CreateFlags, paths ...string) *Stream {
	cpaths := C.CFArrayCreateMutable(nil, 0, &C.kCFTypeArrayCallBacks)
	defer C.CFRelease(C.CFTypeRef(cpaths))

	for _, dir := range paths {
		path := C.CString(dir)
		str := C.CFStringCreateWithCString(nil, path, C.kCFStringEncodingUTF8)
		C.free(unsafe.Pointer(path))
		C.CFArrayAppendValue(cpaths, unsafe.Pointer(str))
		C.CFRelease(C.CFTypeRef(str))
	}

	s := &Stream{
		Chan: make(chan Event, 20),
	}

	ctx := C.FSEventStreamContext{info: unsafe.Pointer(&s.Chan)}
	csince := C.FSEventStreamEventId(since)
	cinterval := C.CFTimeInterval(interval / time.Second)
	cflags := C.FSEventStreamCreateFlags(flags & ^useCFTypes)
	s.cstream = C.fsevent_create(&ctx, cpaths, csince, cinterval, cflags)
	return s
}

// Paths ...
func (s Stream) Paths() []string {
	cpaths := C.FSEventStreamCopyPathsBeingWatched(s.cstream)
	defer C.CFRelease(C.CFTypeRef(cpaths))

	count := C.CFArrayGetCount(cpaths)
	paths := make([]string, count)
	for i := C.CFIndex(0); i < count; i++ {
		cstr := C.CFStringRef(C.CFArrayGetValueAtIndex(cpaths, i))
		size := 1024
		for {
			buf := (*C.char)(C.malloc(C.size_t(size)))
			ok := C.CFStringGetCString(cstr, buf, C.CFIndex(size), C.kCFStringEncodingUTF8)
			if ok == C.FALSE {
				C.free(unsafe.Pointer(buf))
				continue
			}
			paths[i] = C.GoString(buf)
			C.free(unsafe.Pointer(buf))
			break
		}
	}
	return paths
}

// Start ...
func (s *Stream) Start() bool {
	successChan := make(chan C.CFRunLoopRef)

	go func() {
		C.FSEventStreamScheduleWithRunLoop(s.cstream,
			C.CFRunLoopGetCurrent(), C.kCFRunLoopCommonModes)
		ok := C.FSEventStreamStart(s.cstream) != C.FALSE
		if ok {
			successChan <- C.CFRunLoopGetCurrent()
			C.CFRunLoopRun()
		} else {
			successChan <- nil
		}
	}()

	runloop := <-successChan

	if runloop == nil {
		return false
	}
	s.runloop = runloop
	return true
}

// Flush ...
func (s Stream) Flush() {
	C.FSEventStreamFlushSync(s.cstream)
}

// FlushAsync ...
func (s Stream) FlushAsync() EventID {
	return EventID(C.FSEventStreamFlushAsync(s.cstream))
}

// Close ...
func (s Stream) Close() {
	C.FSEventStreamStop(s.cstream)
	C.FSEventStreamInvalidate(s.cstream)
	C.FSEventStreamRelease(s.cstream)
	C.CFRunLoopStop(s.runloop)
}

//export goCallback
func goCallback(stream C.FSEventStreamRef, info unsafe.Pointer,
	count C.size_t, paths **C.char, flags *C.FSEventStreamEventFlags,
	ids *C.FSEventStreamEventId) {

	ch := *((*chan Event)(info))

	for i := 0; i < int(count); i++ {
		cpaths := uintptr(unsafe.Pointer(paths)) + (uintptr(i) * unsafe.Sizeof(*paths))
		cpath := *(**C.char)(unsafe.Pointer(cpaths))

		cflags := uintptr(unsafe.Pointer(flags)) + (uintptr(i) * unsafe.Sizeof(*flags))
		cflag := *(*C.FSEventStreamEventFlags)(unsafe.Pointer(cflags))

		cids := uintptr(unsafe.Pointer(ids)) + (uintptr(i) * unsafe.Sizeof(*ids))
		cid := *(*C.FSEventStreamEventId)(unsafe.Pointer(cids))

		ch <- Event{
			ID:    EventID(cid),
			Path:  C.GoString(cpath),
			Flags: EventFlags(cflag),
		}
	}
}
