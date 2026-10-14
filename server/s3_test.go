// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestMakeS3Client(t *testing.T) {
	for _, tc := range []struct {
		name, global, specific, ignore, wantEndpoint string
		pathStyle                                    bool
	}{
		{name: "aws-default"},
		{name: "global-endpoint", global: "https://global.example.invalid", wantEndpoint: "https://global.example.invalid"},
		{name: "s3-endpoint", specific: "https://s3.example.invalid", wantEndpoint: "https://s3.example.invalid"},
		{name: "s3-takes-precedence", global: "https://global.example.invalid", specific: "https://s3.example.invalid", wantEndpoint: "https://s3.example.invalid"},
		{name: "ignore-configured-endpoints", specific: "https://s3.example.invalid", ignore: "true"},
		{name: "path-style", specific: "https://s3.example.invalid", wantEndpoint: "https://s3.example.invalid", pathStyle: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configFile := filepath.Join(t.TempDir(), "aws-config")
			if err := os.WriteFile(configFile, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AWS_CONFIG_FILE", configFile)
			t.Setenv("AWS_SHARED_CREDENTIALS_FILE", configFile)
			t.Setenv("AWS_PROFILE", "")
			t.Setenv("AWS_DEFAULT_PROFILE", "")
			t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
			t.Setenv("AWS_ENDPOINT_URL", tc.global)
			t.Setenv("AWS_ENDPOINT_URL_S3", tc.specific)
			t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", tc.ignore)
			client, err := makeS3Client(context.Background(), "us-east-1", "test-bucket", "", tc.pathStyle)
			if err != nil {
				t.Fatal(err)
			}
			opts := client.Options()
			if got := aws.ToString(opts.BaseEndpoint); got != tc.wantEndpoint {
				t.Errorf("BaseEndpoint = %q, want %q", got, tc.wantEndpoint)
			}
			if opts.UsePathStyle != tc.pathStyle {
				t.Errorf("UsePathStyle = %v, want %v", opts.UsePathStyle, tc.pathStyle)
			}
		})
	}
}

type s3TestTransport func(*http.Request) (*http.Response, error)

func (f s3TestTransport) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestPathStylePutObject(t *testing.T) {
	for _, pathStyle := range []bool{false, true} {
		name := "virtual-host"
		if pathStyle {
			name = "path-style"
		}
		t.Run(name, func(t *testing.T) {
			var gotHost, gotPath string
			client := s3.New(s3.Options{
				Region:       "us-east-1",
				Credentials:  aws.AnonymousCredentials{},
				BaseEndpoint: aws.String("https://storage.example.invalid"),
				UsePathStyle: pathStyle,
				HTTPClient: s3TestTransport(func(r *http.Request) (*http.Response, error) {
					gotHost, gotPath = r.URL.Host, r.URL.Path
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
				}),
			})
			_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
				Bucket: aws.String("test-bucket"), Key: aws.String("snapshot"), Body: bytes.NewReader([]byte("test")),
			})
			if err != nil {
				t.Fatal(err)
			}
			wantHost, wantPath := "test-bucket.storage.example.invalid", "/snapshot"
			if pathStyle {
				wantHost, wantPath = "storage.example.invalid", "/test-bucket/snapshot"
			}
			if gotHost != wantHost || gotPath != wantPath {
				t.Errorf("request = %s%s, want %s%s", gotHost, gotPath, wantHost, wantPath)
			}
		})
	}
}
