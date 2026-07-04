package auth

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDecodeUserInfo_OK(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"123","email":"a@b.com","name":"A","picture":"p"}`)),
	}
	info, err := decodeUserInfo(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "123" || info.Email != "a@b.com" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestDecodeUserInfo_NonOKStatusReturnsError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader(`{"error":"insufficient scope"}`)),
	}
	info, err := decodeUserInfo(resp)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
	if info != nil {
		t.Fatalf("expected nil info on error, got %+v", info)
	}
}
