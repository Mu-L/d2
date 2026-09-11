package imageasset

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/d2lang/d2/lib/netpolicy"
)

func TestHTTPNetworkPolicy(t *testing.T) {
	image := encodePNG(t, 2, 3)
	var imageHits atomic.Int32
	var firstRedirectHits atomic.Int32
	var secondRedirectHits atomic.Int32
	var privateHits atomic.Int32

	var port string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/image":
			imageHits.Add(1)
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(image)
		case "/redirect-one":
			firstRedirectHits.Add(1)
			http.Redirect(response, request, "http://1.1.1.1:"+port+"/redirect-two", http.StatusFound)
		case "/redirect-two":
			secondRedirectHits.Add(1)
			http.Redirect(response, request, "http://127.0.0.1:"+port+"/private", http.StatusFound)
		case "/private":
			privateHits.Add(1)
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(image)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	parsedServer, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err = net.SplitHostPort(parsedServer.Host)
	if err != nil {
		t.Fatal(err)
	}
	client := mappedPublicClient(parsedServer.Host, map[string]bool{"8.8.8.8": true, "1.1.1.1": true})

	newResolver := func(t *testing.T) *Resolver {
		t.Helper()
		resolver, err := New(Options{HTTPClient: client, Limits: generousLimits()})
		if err != nil {
			t.Fatal(err)
		}
		return resolver
	}

	t.Run("allows public target", func(t *testing.T) {
		resource, err := newResolver(t).Resolve(context.Background(), "http://8.8.8.8:"+port+"/image")
		if err != nil {
			t.Fatal(err)
		}
		assertResource(t, resource, KindRaster, "image/png", 2, 3)
		if imageHits.Load() != 1 {
			t.Fatalf("image endpoint received %d requests", imageHits.Load())
		}
	})

	t.Run("blocks direct private target", func(t *testing.T) {
		_, err := newResolver(t).Resolve(context.Background(), server.URL+"/private")
		if !errors.Is(err, netpolicy.ErrBlockedAddress) {
			t.Fatalf("Resolve error = %v, want ErrBlockedAddress", err)
		}
		if privateHits.Load() != 0 {
			t.Fatalf("private endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("checks every redirect", func(t *testing.T) {
		_, err := newResolver(t).Resolve(context.Background(), "http://8.8.8.8:"+port+"/redirect-one")
		if !errors.Is(err, netpolicy.ErrBlockedAddress) {
			t.Fatalf("Resolve error = %v, want ErrBlockedAddress", err)
		}
		if firstRedirectHits.Load() != 1 || secondRedirectHits.Load() != 1 {
			t.Fatalf("redirect hits = %d/%d, want 1/1", firstRedirectHits.Load(), secondRedirectHits.Load())
		}
		if privateHits.Load() != 0 {
			t.Fatalf("private redirect endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("explicit opt-in allows trusted private target", func(t *testing.T) {
		resolver, err := New(Options{
			HTTPClient:    server.Client(),
			NetworkPolicy: netpolicy.Policy{AllowPrivateNetworks: true},
			Limits:        generousLimits(),
		})
		if err != nil {
			t.Fatal(err)
		}
		before := privateHits.Load()
		resource, err := resolver.Resolve(context.Background(), server.URL+"/private")
		if err != nil {
			t.Fatal(err)
		}
		assertResource(t, resource, KindRaster, "image/png", 2, 3)
		if privateHits.Load() != before+1 {
			t.Fatalf("private endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("cache is separated by policy", func(t *testing.T) {
		cache, err := NewMemoryCache(4, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		trusted, err := New(Options{
			HTTPClient:     server.Client(),
			NetworkPolicy:  netpolicy.Policy{AllowPrivateNetworks: true},
			Cache:          cache,
			CacheNamespace: "shared-network-test",
			Limits:         generousLimits(),
		})
		if err != nil {
			t.Fatal(err)
		}
		before := privateHits.Load()
		if _, err := trusted.Resolve(context.Background(), server.URL+"/private"); err != nil {
			t.Fatal(err)
		}
		publicOnly, err := New(Options{
			HTTPClient:     server.Client(),
			Cache:          cache,
			CacheNamespace: "shared-network-test",
			Limits:         generousLimits(),
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = publicOnly.Resolve(context.Background(), server.URL+"/private")
		if !errors.Is(err, netpolicy.ErrBlockedAddress) {
			t.Fatalf("Resolve error = %v, want ErrBlockedAddress", err)
		}
		if privateHits.Load() != before+1 {
			t.Fatalf("private endpoint received %d new requests, want one trusted request", privateHits.Load()-before)
		}
	})
}

func mappedPublicClient(localAddress string, allowed map[string]bool) *http.Client {
	transport := &http.Transport{}
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if !allowed[host] {
			return nil, fmt.Errorf("test dialer received unexpected address %s", address)
		}
		return dialer.DialContext(ctx, network, localAddress)
	}
	return &http.Client{Transport: transport}
}
