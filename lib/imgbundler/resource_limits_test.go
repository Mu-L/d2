package imgbundler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/simplelog"
)

func TestBundleCapsImageReferences(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "image.svg")
	if err := os.WriteFile(imagePath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o600); err != nil {
		t.Fatal(err)
	}

	makeSource := func(references int) []byte {
		var source strings.Builder
		source.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
		for i := 0; i < references; i++ {
			fmt.Fprintf(&source, `<image href="%s"/>`, imagePath)
		}
		source.WriteString(`</svg>`)
		return []byte(source.String())
	}

	ctx := log.With(context.Background(), testlog.New(t))
	if _, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", makeSource(maxImageReferences), localfile.Unrestricted(), false); err != nil {
		t.Fatalf("inclusive reference limit failed: %v", err)
	}
	_, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", makeSource(maxImageReferences+1), localfile.Unrestricted(), false)
	if err == nil {
		t.Fatal("BundleLocal accepted more than 4,096 image references")
	}
	if !strings.Contains(err.Error(), "image references exceed maximum of 4096") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func imageCacheStats(cache *imageCache) (entries int, bytes int64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.entries), cache.bytes
}

func TestApplyReplacementsCapsOutputAndHandlesDuplicates(t *testing.T) {
	source := []byte(`<svg><image href="same"/><g/><image href="same"/></svg>`)
	matches, hrefs, err := findImageElements(context.Background(), source, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || len(hrefs) != 1 {
		t.Fatalf("got %d matches and %d unique hrefs", len(matches), len(hrefs))
	}
	replacement := []byte(`<image href="data:image/png;base64,AAAA"`)
	want := bytes.ReplaceAll(source, []byte(`<image href="same"`), replacement)

	output, err := applyReplacements(context.Background(), source, matches, map[string][]byte{"same": replacement}, int64(len(want)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, want) {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
	_, err = applyReplacements(context.Background(), source, matches, map[string][]byte{"same": replacement}, int64(len(want)-1))
	if err == nil || !strings.Contains(err.Error(), "bundled SVG output exceeds maximum") {
		t.Fatalf("expected output limit error, got %v", err)
	}
}

func TestBundleBudgetIsCumulativeAndSticky(t *testing.T) {
	budget := &bundleBudget{maxFetchedBytes: 6, maxSourceBytes: 6, maxBundledBytes: 6}
	if err := budget.reserveBundled(4); err != nil {
		t.Fatal(err)
	}
	if err := budget.reserveBundled(3); err == nil {
		t.Fatal("budget accepted cumulative bytes above its limit")
	}
	if err := budget.reserveBundled(2); err == nil {
		t.Fatal("budget continued after its first limit error")
	}
	if err := budget.reserveSource(1); err == nil {
		t.Fatal("one exhausted dimension did not stop the operation")
	}
}

func TestRunWorkersEnforcesCumulativeBundleBudget(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "image.svg")
	if err := os.WriteFile(imagePath, []byte(`<svg/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := localPolicySVG(imagePath)
	matches, hrefs, err := findImageElements(context.Background(), source, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		budget      *bundleBudget
		wantMessage string
	}{
		{name: "fetched bytes", budget: &bundleBudget{maxFetchedBytes: 1, maxSourceBytes: 1 << 20, maxBundledBytes: 1 << 20}, wantMessage: "cumulative fetched image bytes exceeds maximum"},
		{name: "source bytes", budget: &bundleBudget{maxFetchedBytes: 1 << 20, maxSourceBytes: 1, maxBundledBytes: 1 << 20}, wantMessage: "cumulative source image bytes exceeds maximum"},
		{name: "bundled bytes", budget: &bundleBudget{maxFetchedBytes: 1 << 20, maxSourceBytes: 1 << 20, maxBundledBytes: 1}, wantMessage: "cumulative bundled image bytes exceeds maximum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := log.With(context.Background(), testlog.New(t))
			output, err := runWorkers(ctx, simplelog.FromLibLog(ctx), "-", source, matches, hrefs, localfile.Unrestricted(), false, false, nil, netpolicy.Policy{}, tc.budget)
			if err == nil || !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("expected cumulative byte limit error, got %v", err)
			}
			if !bytes.Equal(output, source) {
				t.Fatal("failed resource changed output")
			}
		})
	}
}

func TestAggregateLimitStopsSchedulingNewFetches(t *testing.T) {
	var source strings.Builder
	source.WriteString(`<svg>`)
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&source, `<image href="https://example.com/%d"/>`, i)
	}
	source.WriteString(`</svg>`)
	sourceBytes := []byte(source.String())
	matches, hrefs, err := findImageElements(context.Background(), sourceBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) *http.Response {
		requests.Add(1)
		response := httptest.NewRecorder()
		response.Header().Set("Content-Type", "image/png")
		response.WriteHeader(http.StatusOK)
		_, _ = response.WriteString("xx")
		return response.Result()
	})}
	ctx := log.With(context.Background(), testlog.New(t))
	_, err = runWorkers(ctx, simplelog.FromLibLog(ctx), "-", sourceBytes, matches, hrefs, localfile.Policy{}, true, false, client, netpolicy.Policy{}, &bundleBudget{maxFetchedBytes: 1, maxSourceBytes: 1 << 20, maxBundledBytes: 1 << 20})
	if err == nil || !strings.Contains(err.Error(), "cumulative fetched image bytes exceeds maximum") {
		t.Fatalf("expected cumulative fetch limit error, got %v", err)
	}
	if got := requests.Load(); got > maxImageWorkers {
		t.Fatalf("aggregate failure allowed %d requests, worker ceiling %d", got, maxImageWorkers)
	}
}

func TestWorkerChargesCacheHitToBundleBudget(t *testing.T) {
	previousCache := imgCache
	imgCache = newImageCache(maxImageCacheEntries, maxImageCacheBytes)
	t.Cleanup(func() { imgCache = previousCache })
	href := "https://example.com/image.png"
	value := []byte(`<image href="data:image/png;base64,AAAA"`)
	imgCache.Store(imageCacheKey{href: href, isRemote: true}, value)

	ctx := log.With(context.Background(), testlog.New(t))
	_, err := worker(ctx, simplelog.FromLibLog(ctx), "", href, localfile.Policy{}, true, true, nil, netpolicy.Policy{}, &bundleBudget{maxFetchedBytes: 1, maxSourceBytes: 1, maxBundledBytes: int64(len(value) - 1)})
	if err == nil || !strings.Contains(err.Error(), "cumulative bundled image bytes exceeds maximum") {
		t.Fatalf("expected cached value to consume aggregate budget, got %v", err)
	}
	output, err := worker(ctx, simplelog.FromLibLog(ctx), "", href, localfile.Policy{}, true, true, nil, netpolicy.Policy{}, &bundleBudget{maxFetchedBytes: 1, maxSourceBytes: 1, maxBundledBytes: int64(len(value))})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, value) {
		t.Fatal("cache hit changed value")
	}
}

func TestImageCacheIsBoundedLRU(t *testing.T) {
	cache := newImageCache(2, 1<<20)
	first := imageCacheKey{href: "first"}
	second := imageCacheKey{href: "second"}
	third := imageCacheKey{href: "third"}
	cache.Store(first, []byte("1"))
	cache.Store(second, []byte("2"))
	if _, ok := cache.Load(first); !ok {
		t.Fatal("first cache entry unexpectedly missing")
	}
	cache.Store(third, []byte("3"))
	if _, ok := cache.Load(second); ok {
		t.Fatal("cache did not evict its least-recently-used entry")
	}
	if entries, _ := imageCacheStats(cache); entries != 2 {
		t.Fatalf("cache retained %d entries, want 2", entries)
	}

	entryBytes := imageCacheEntryBytes(first, []byte("1234"))
	byteCache := newImageCache(2, entryBytes)
	byteCache.Store(first, []byte("1234"))
	other := imageCacheKey{href: "other"}
	byteCache.Store(other, []byte("1234"))
	entries, bytes := imageCacheStats(byteCache)
	if entries != 1 || bytes > entryBytes {
		t.Fatalf("byte-bounded cache retained %d entries and %d bytes", entries, bytes)
	}
	if _, ok := byteCache.Load(first); ok {
		t.Fatal("byte-bounded cache did not evict its oldest entry")
	}
	if _, ok := byteCache.Load(other); !ok {
		t.Fatal("byte-bounded cache did not retain the replacement entry")
	}
}

func TestConfiguredImageCacheCapsEntries(t *testing.T) {
	cache := newImageCache(maxImageCacheEntries, maxImageCacheBytes)
	for i := 0; i < maxImageCacheEntries+1; i++ {
		cache.Store(imageCacheKey{href: fmt.Sprintf("image-%d", i)}, []byte("x"))
	}
	entries, bytes := imageCacheStats(cache)
	if entries != maxImageCacheEntries {
		t.Fatalf("cache retained %d entries, want %d", entries, maxImageCacheEntries)
	}
	if bytes > maxImageCacheBytes {
		t.Fatalf("cache retained %d bytes, limit %d", bytes, maxImageCacheBytes)
	}
}

func TestBundleSkipsLargeDataURI(t *testing.T) {
	href := "data:image/png;base64," + strings.Repeat("A", maxImageReferenceBytes)
	source := []byte(fmt.Sprintf(`<svg><image href="%s"/></svg>`, href))
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := BundleRemote(ctx, simplelog.FromLibLog(ctx), source, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, source) {
		t.Fatal("already-bundled data URI changed")
	}
}

func TestBundleIgnoresOversizedReferencesForOtherPass(t *testing.T) {
	remoteHref := "https://example.com/" + strings.Repeat("a", maxImageReferenceBytes)
	source := []byte(fmt.Sprintf(`<svg><image href="%s"/></svg>`, remoteHref))
	ctx := log.With(context.Background(), testlog.New(t))
	output, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", source, localfile.Unrestricted(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output, source) {
		t.Fatal("remote reference changed during local bundling")
	}
}

func TestBundleCapsEligibleReferenceBytes(t *testing.T) {
	href := strings.Repeat("a", maxImageReferenceBytes+1)
	source := []byte(fmt.Sprintf(`<svg><image href="%s"/></svg>`, href))
	ctx := log.With(context.Background(), testlog.New(t))
	_, err := BundleLocalWithPolicy(ctx, simplelog.FromLibLog(ctx), "-", source, localfile.Unrestricted(), false)
	if err == nil || !strings.Contains(err.Error(), "image reference exceeds maximum") {
		t.Fatalf("expected image-reference limit error, got %v", err)
	}
}
