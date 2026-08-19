package main

import (
	"io"
	"net/http"
	"testing"
)

type campaignHealthBody struct{ closed bool }

func (b *campaignHealthBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *campaignHealthBody) Close() error             { b.closed = true; return nil }

type campaignHealthRoundTripper func(*http.Request) (*http.Response, error)

func (f campaignHealthRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHealthcheckOKClosesNonOKResponse(t *testing.T) {
	body := &campaignHealthBody{}
	client := &http.Client{Transport: campaignHealthRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body, Header: make(http.Header)}, nil
	})}
	if healthcheckOK(client, "http://health.invalid") {
		t.Fatal("non-OK health response was accepted")
	}
	if !body.closed {
		t.Fatal("non-OK health response body was not closed")
	}
}
