package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestShutdownServerCancelsRequestsAfterTimeout(t *testing.T) {
	baseContext, cancelRequests := context.WithCancel(context.Background())
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	server := &http.Server{
		BaseContext: func(net.Listener) context.Context { return baseContext },
		Handler: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			close(requestStarted)
			<-request.Context().Done()
			close(requestCanceled)
		}),
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	clientDone := make(chan struct{})
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			response.Body.Close()
		}
		close(clientDone)
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}
	startedAt := time.Now()
	err = shutdownServer(server, cancelRequests, 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdownServer() error = %v, want deadline exceeded", err)
	}
	if time.Since(startedAt) > time.Second {
		t.Fatal("shutdownServer() did not honor its timeout")
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("in-flight request context was not canceled")
	}
	select {
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Serve() error = %v, want server closed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve() did not stop")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("client request did not stop")
	}
}
