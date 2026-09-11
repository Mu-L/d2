package netpolicy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestNonPublicAddressRanges(t *testing.T) {
	t.Parallel()
	for _, address := range []string{
		"0.0.0.1",
		"10.0.0.1",
		"100.100.100.200",
		"127.0.0.1",
		"168.63.129.16",
		"169.254.169.254",
		"172.16.0.1",
		"192.168.0.1",
		"198.18.0.1",
		"224.0.0.1",
		"255.255.255.255",
		"::",
		"::1",
		"::ffff:127.0.0.1",
		"64:ff9b::7f00:1",
		"2001::1",
		"2002:7f00:1::",
		"fc00::1",
		"fd00:ec2::254",
		"fe80::1",
		"ff02::1",
		"fec0::1",
	} {
		t.Run(address, func(t *testing.T) {
			if isPublicAddress(netip.MustParseAddr(address).Unmap()) {
				t.Fatalf("isPublicAddress(%s) = true", address)
			}
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		t.Run("public_"+address, func(t *testing.T) {
			if !isPublicAddress(netip.MustParseAddr(address)) {
				t.Fatalf("isPublicAddress(%s) = false", address)
			}
		})
	}
}

func TestValidatedDialUsesOnlyResolvedNumericAddress(t *testing.T) {
	t.Parallel()
	lookupCalls := 0
	resolver := resolverFunc(func(_ context.Context, network, host string) ([]netip.Addr, error) {
		lookupCalls++
		if network != "ip" || host != "assets.example" {
			t.Fatalf("LookupNetIP(%q, %q)", network, host)
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	wantDialError := errors.New("dial stopped by test")
	var dialedAddress string
	base := &http.Client{Transport: &http.Transport{
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("network = %q", network)
			}
			dialedAddress = address
			return nil, wantDialError
		},
	}}
	client, err := newHTTPClient(base, Policy{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "assets.example:443")
	if !errors.Is(err, wantDialError) {
		t.Fatalf("DialContext error = %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("lookup calls = %d, want 1", lookupCalls)
	}
	if dialedAddress != "93.184.216.34:443" {
		t.Fatalf("dialed address = %q, want validated numeric address", dialedAddress)
	}
}

func TestValidatedDialRejectsMixedResolutionBeforeDial(t *testing.T) {
	t.Parallel()
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("93.184.216.34"),
			netip.MustParseAddr("127.0.0.1"),
		}, nil
	})
	dialed := false
	base := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, errors.New("unexpected dial")
		},
	}}
	client, err := newHTTPClient(base, Policy{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "assets.example:80")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("DialContext error = %v, want ErrBlockedAddress", err)
	}
	if dialed {
		t.Fatal("transport dialer was called for a mixed public/private resolution")
	}
}

func TestPrivateNetworkOptInPreservesCustomRoundTripper(t *testing.T) {
	t.Parallel()
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("stopped by test")
	})
	base := &http.Client{Transport: transport}
	client, err := NewHTTPClient(base, Policy{AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Transport.(roundTripperFunc); !ok {
		t.Fatal("trusted opt-in replaced the caller's custom transport")
	}
	if _, err := NewHTTPClient(base, Policy{}); err == nil {
		t.Fatal("public-only policy accepted a custom protocol RoundTripper")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
