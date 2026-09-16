package server

import (
	"net"
	"testing"
)

func TestWriteListenerInfoRequiresAccessToken(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	if err := WriteListenerInfo(listener, ListenerSpec{Addr: "127.0.0.1:0"}); err == nil {
		t.Fatal("expected missing access token to fail")
	}
}
