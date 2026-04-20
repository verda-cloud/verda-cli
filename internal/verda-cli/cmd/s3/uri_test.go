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

package s3

import "testing"

func TestParseS3URI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		in         string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		{"bucket only", "s3://my-bucket", "my-bucket", "", false},
		{"bucket trailing slash", "s3://my-bucket/", "my-bucket", "", false},
		{"bucket and key", "s3://my-bucket/path/file.txt", "my-bucket", "path/file.txt", false},
		{"nested key", "s3://b/a/b/c/d.txt", "b", "a/b/c/d.txt", false},
		{"url-encoded key", "s3://b/hello%20world.txt", "b", "hello world.txt", false},
		{"missing scheme", "my-bucket/key", "", "", true},
		{"empty bucket", "s3:///key", "", "", true},
		{"empty input", "", "", "", true},
		{"wrong scheme", "https://foo/bar", "", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseS3URI(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got.Bucket != tc.wantBucket {
				t.Errorf("Bucket = %q, want %q", got.Bucket, tc.wantBucket)
			}
			if got.Key != tc.wantKey {
				t.Errorf("Key = %q, want %q", got.Key, tc.wantKey)
			}
		})
	}
}

func TestIsS3URI(t *testing.T) {
	t.Parallel()
	if !IsS3URI("s3://bucket/key") {
		t.Error("expected true for s3:// URI")
	}
	if IsS3URI("/local/path") {
		t.Error("expected false for local path")
	}
	if IsS3URI("") {
		t.Error("expected false for empty string")
	}
}
