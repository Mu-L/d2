package imgbundler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/simplelog"
)

func TestBundleRemoteNetworkPolicy(t *testing.T) {
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
			_, _ = response.Write(testPNGFile)
		case "/redirect-one":
			firstRedirectHits.Add(1)
			http.Redirect(response, request, "http://1.1.1.1:"+port+"/redirect-two", http.StatusFound)
		case "/redirect-two":
			secondRedirectHits.Add(1)
			http.Redirect(response, request, "http://127.0.0.1:"+port+"/private", http.StatusFound)
		case "/private":
			privateHits.Add(1)
			response.Header().Set("Content-Type", "image/png")
			_, _ = response.Write(testPNGFile)
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

	previousClient := httpClient
	httpClient = mappedPublicClient(parsedServer.Host, map[string]bool{"8.8.8.8": true, "1.1.1.1": true})
	t.Cleanup(func() { httpClient = previousClient })
	ctx := log.With(context.Background(), testlog.New(t))
	logger := simplelog.FromLibLog(ctx)

	t.Run("allows public target", func(t *testing.T) {
		output, err := BundleRemote(ctx, logger, remoteImageSVG("http://8.8.8.8:"+port+"/image"), false)
		if err != nil {
			t.Fatal(err)
		}
		if imageHits.Load() != 1 || !strings.Contains(string(output), "data:image/png;base64,") {
			t.Fatalf("image hits = %d, output = %s", imageHits.Load(), output)
		}
	})

	t.Run("blocks direct private target", func(t *testing.T) {
		_, err := BundleRemote(ctx, logger, remoteImageSVG(server.URL+"/private"), false)
		if err == nil {
			t.Fatal("expected direct private-network request to fail")
		}
		if privateHits.Load() != 0 {
			t.Fatalf("private endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("checks every redirect", func(t *testing.T) {
		_, err := BundleRemote(ctx, logger, remoteImageSVG("http://8.8.8.8:"+port+"/redirect-one"), false)
		if err == nil {
			t.Fatal("expected redirect to private network to fail")
		}
		if firstRedirectHits.Load() != 1 || secondRedirectHits.Load() != 1 {
			t.Fatalf("redirect hits = %d/%d, want 1/1", firstRedirectHits.Load(), secondRedirectHits.Load())
		}
		if privateHits.Load() != 0 {
			t.Fatalf("private redirect endpoint received %d requests", privateHits.Load())
		}
	})

	t.Run("explicit opt-in allows trusted private target", func(t *testing.T) {
		mappedClient := httpClient
		httpClient = server.Client()
		defer func() { httpClient = mappedClient }()
		before := privateHits.Load()
		output, err := BundleRemoteWithPolicy(ctx, logger, remoteImageSVG(server.URL+"/private"), false, netpolicy.Policy{AllowPrivateNetworks: true})
		if err != nil {
			t.Fatal(err)
		}
		if privateHits.Load() != before+1 || !strings.Contains(string(output), "data:image/png;base64,") {
			t.Fatalf("private hits = %d, output = %s", privateHits.Load(), output)
		}
	})

	t.Run("cache is separated by policy", func(t *testing.T) {
		imgCache = sync.Map{}
		mappedClient := httpClient
		httpClient = server.Client()
		before := privateHits.Load()
		_, err := BundleRemoteWithPolicy(ctx, logger, remoteImageSVG(server.URL+"/private"), true, netpolicy.Policy{AllowPrivateNetworks: true})
		if err != nil {
			httpClient = mappedClient
			t.Fatal(err)
		}
		httpClient = mappedClient
		_, err = BundleRemote(ctx, logger, remoteImageSVG(server.URL+"/private"), true)
		if err == nil {
			t.Fatal("public-only request reused a private-policy cache entry")
		}
		if privateHits.Load() != before+1 {
			t.Fatalf("private endpoint received %d new requests, want one trusted request", privateHits.Load()-before)
		}
	})
}

func remoteImageSVG(source string) []byte {
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg"><image href="%s"/></svg>`, source))
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
