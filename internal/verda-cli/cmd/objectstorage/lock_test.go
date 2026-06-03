// Copyright 2026 Verda Cloud Oy
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

//go:build !windows

package objectstorage

import "testing"

// TestAcquireUploadLock_ExclusiveThenReleasable verifies the same-host guard:
// a held lock blocks a second acquire, and releasing frees it.
func TestAcquireUploadLock_ExclusiveThenReleasable(t *testing.T) {
	withTempVerdaHome(t)
	const id = "deadbeef"

	release, acquired, err := acquireTransferLock(id)
	if err != nil || !acquired {
		t.Fatalf("first acquire: acquired=%v err=%v", acquired, err)
	}

	if _, ok, err2 := acquireTransferLock(id); err2 != nil || ok {
		t.Errorf("second acquire while held: ok=%v err=%v, want ok=false", ok, err2)
	}

	release()

	release2, ok3, err3 := acquireTransferLock(id)
	if err3 != nil || !ok3 {
		t.Fatalf("re-acquire after release: ok=%v err=%v", ok3, err3)
	}
	release2()
}
