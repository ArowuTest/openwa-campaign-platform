package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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
