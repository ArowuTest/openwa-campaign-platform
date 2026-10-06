package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeS3Object struct {
	body []byte
	hash string
}

func TestS3StoreRoundTripAndExactReplay(t *testing.T) {
	var mu sync.Mutex
	objects := map[string]fakeS3Object{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") || r.Header.Get("X-Amz-Date") == "" {
			http.Error(w, "unsigned", http.StatusUnauthorized)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/bucket/")
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			if _, ok := objects[key]; ok && r.Header.Get("If-None-Match") == "*" {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			body, _ := io.ReadAll(r.Body)
			objects[key] = fakeS3Object{body: body, hash: r.Header.Get("X-Amz-Meta-Sha256")}
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			object, ok := objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", stringInt64(int64(len(object.body))))
			w.Header().Set("X-Amz-Meta-Size", stringInt64(int64(len(object.body))))
			w.Header().Set("X-Amz-Meta-Sha256", object.hash)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			object, ok := objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", stringInt64(int64(len(object.body))))
			w.Header().Set("X-Amz-Meta-Sha256", object.hash)
			_, _ = w.Write(object.body)
		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	store, err := NewS3Store(S3Options{Endpoint: server.URL, Bucket: "bucket", Region: "test-1", AccessKey: "access", SecretKey: "secret", PathStyle: true, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := store.Put(context.Background(), "reports/my report.json", bytes.NewBufferString("evidence"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Size != 8 {
		t.Fatalf("size=%d", metadata.Size)
	}
	if _, err = store.Put(context.Background(), "reports/my report.json", bytes.NewBufferString("evidence"), 100); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if _, err = store.Put(context.Background(), "reports/my report.json", bytes.NewBufferString("different"), 100); err != ErrKeyConflict {
		t.Fatalf("conflict=%v", err)
	}
	object, opened, err := store.Open(context.Background(), "reports/my report.json")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(object)
	_ = object.Close()
	if string(content) != "evidence" || opened.SHA256 != metadata.SHA256 {
		t.Fatalf("opened=%q %+v", content, opened)
	}
	if err = store.Delete(context.Background(), "reports/my report.json"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Stat(context.Background(), "reports/my report.json"); err != ErrNotFound {
		t.Fatalf("stat after delete=%v", err)
	}
}

func TestS3StoreCreatesShortLivedDirectPartTargetWithoutSecretLeak(t *testing.T) {
	now := time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC)
	store, err := NewS3Store(S3Options{
		Endpoint:             "https://s3-internal.example.test",
		DirectUploadEndpoint: "https://s3-upload.example.test",
		Bucket:               "private-bucket",
		Region:               "eu-west-2",
		AccessKey:            "ACCESSKEY",
		SecretKey:            "super-secret-do-not-leak",
		SessionToken:         "session-token-value",
		PathStyle:            true,
		DirectUploadAllowed:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	store.Now = func() time.Time { return now }
	checksum := strings.Repeat("a", 64)
	target, err := store.CreateDirectUploadTarget(context.Background(), "imports/uploads/session/part-000001.bin", 8<<20, checksum, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if target.Method != http.MethodPut || target.ExpectedBytes != 8<<20 || !target.ExpiresAt.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("target=%+v", target)
	}
	if strings.Contains(target.URL, store.SecretKey) {
		t.Fatal("presigned target leaked reusable S3 secret")
	}
	parsed, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "s3-upload.example.test" || parsed.Path != "/private-bucket/imports/uploads/session/part-000001.bin" {
		t.Fatalf("direct target host/path=%q %q", parsed.Host, parsed.Path)
	}
	query := parsed.Query()
	if query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" ||
		query.Get("X-Amz-Date") != "20261006T130000Z" ||
		query.Get("X-Amz-Expires") != "300" ||
		query.Get("X-Amz-Security-Token") != "session-token-value" ||
		!strings.HasPrefix(query.Get("X-Amz-Credential"), "ACCESSKEY/20261006/eu-west-2/s3/aws4_request") ||
		len(query.Get("X-Amz-Signature")) != 64 {
		t.Fatalf("unexpected presign query: %s", parsed.RawQuery)
	}
	signed := query.Get("X-Amz-SignedHeaders")
	for _, required := range []string{"host", "if-none-match", "x-amz-content-sha256", "x-amz-meta-created-at", "x-amz-meta-sha256", "x-amz-meta-size"} {
		if !strings.Contains(signed, required) {
			t.Fatalf("signed headers %q missing %q", signed, required)
		}
	}
	if target.Headers["If-None-Match"] != "*" ||
		target.Headers["X-Amz-Content-Sha256"] != unsignedPayloadSHA256 ||
		target.Headers["X-Amz-Meta-Sha256"] != checksum ||
		target.Headers["X-Amz-Meta-Size"] != stringInt64(8<<20) ||
		target.Headers["Content-Type"] != "application/octet-stream" {
		t.Fatalf("target headers=%v", target.Headers)
	}
}

func TestS3StoreDirectPartTargetRejectsUnsafeEnvelope(t *testing.T) {
	store, err := NewS3Store(S3Options{
		Endpoint: "https://s3-internal.example.test", DirectUploadEndpoint: "https://s3-upload.example.test",
		Bucket: "private-bucket", Region: "eu-west-2",
		AccessKey: "access", SecretKey: "secret", PathStyle: true, DirectUploadAllowed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	validSHA := strings.Repeat("a", 64)
	if _, err := store.CreateDirectUploadTarget(context.Background(), "imports/uploads/session/part.bin", 0, validSHA, time.Minute); err == nil {
		t.Fatal("zero-byte target accepted")
	}
	if _, err := store.CreateDirectUploadTarget(context.Background(), "imports/uploads/session/part.bin", 10, "not-a-sha", time.Minute); err == nil {
		t.Fatal("invalid SHA target accepted")
	}
	if _, err := store.CreateDirectUploadTarget(context.Background(), "imports/uploads/session/part.bin", 10, validSHA, 16*time.Minute); err == nil {
		t.Fatal("overlong target TTL accepted")
	}
	if _, err := store.CreateDirectUploadTarget(context.Background(), "../escape", 10, validSHA, time.Minute); err == nil {
		t.Fatal("unsafe object key accepted")
	}
}

func stringInt64(value int64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
