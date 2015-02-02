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

#include <CoreServices/CoreServices.h>
#include "_cgo_export.h"

static void fsevent_callback(ConstFSEventStreamRef streamRef,
                             void *clientCallBackInfo,
                             size_t numEvents,
                             void *eventPaths,
                             const FSEventStreamEventFlags eventFlags[],
                             const FSEventStreamEventId eventIds[]) {
  goCallback(
      (FSEventStreamRef)streamRef,
      clientCallBackInfo,
      numEvents,
      eventPaths,
      (FSEventStreamEventFlags*)eventFlags,
      (FSEventStreamEventId*)eventIds);
}

FSEventStreamRef fsevent_create(FSEventStreamContext *ctx,
                                CFMutableArrayRef pathsToWatch,
                                FSEventStreamEventId since,
                                CFTimeInterval latency,
                                FSEventStreamCreateFlags flags) {
  return FSEventStreamCreate(
      NULL,
      fsevent_callback,
      ctx,
      pathsToWatch,
      since,
      latency,
      flags);
}
