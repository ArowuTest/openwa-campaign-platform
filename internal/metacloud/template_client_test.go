package metacloud

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListsAllWABATemplatesAcrossSameOriginPages(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-value-abcdefghijklmnopqrstuvwxyz" {
			t.Fatal("missing bearer auth")
		}
		if r.URL.Path != "/v23.0/waba-123/message_templates" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "page2" {
			_, _ = io.WriteString(w, `{"data":[{"id":"2","name":"order_ready","language":"en_US","category":"UTILITY","status":"APPROVED","components":[]}]}`)
			return
		}
		_, _ = io.WriteString(w, fmt.Sprintf(`{"data":[{"id":"1","name":"sale","language":"en_US","category":"MARKETING","status":"APPROVED","components":[]}],"paging":{"next":%q}}`, server.URL+`/v23.0/waba-123/message_templates?after=page2`))
	}))
	defer server.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: server.URL}
	items, err := client.ListTemplates(context.Background(), TemplateListRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", WABAID: "waba-123"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "sale" || items[1].Name != "order_ready" {
		t.Fatalf("items=%#v", items)
	}
}

func TestClientRejectsCrossOriginTemplatePagination(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Fatal("bearer leaked to pagination target")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, fmt.Sprintf(`{"data":[],"paging":{"next":%q}}`, target.URL+`/capture`))
	}))
	defer source.Close()
	client := &Client{Credentials: testCredentials(t), BaseURL: source.URL}
	_, err := client.ListTemplates(context.Background(), TemplateListRequest{CredentialKey: "meta-ng", GraphAPIVersion: "v23.0", WABAID: "waba-123"})
	if err == nil {
		t.Fatal("cross-origin pagination accepted")
	}
}
