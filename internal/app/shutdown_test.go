package app

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeListenerWaitsForActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, finished := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ServeListener(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()
			time.Sleep(50 * time.Millisecond)
			close(finished)
		}))
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	go func() {
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timed out")
	}
	select {
	case <-finished:
	default:
		t.Fatal("server returned before request stopped")
	}
}

func TestDNSCloseWaitsForObserver(t *testing.T) {
	entered, finished := make(chan struct{}), make(chan struct{})
	d := NewDNS(unusedAddr(t), t.TempDir(), func(ctx context.Context, _ Query) {
		close(entered)
		<-ctx.Done()
		time.Sleep(30 * time.Millisecond)
		close(finished)
	})
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(context.Background()) })
	d.observed <- Query{Domain: "example.com"}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("observer did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("DNS returned before observer stopped")
	}
}
