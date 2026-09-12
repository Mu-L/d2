package imgbundler

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"

	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/netpolicy"
	"github.com/d2lang/d2/lib/simplelog"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/util-go/xdefer"
)

const (
	maxImageSize    int64 = 1 << 25 // 33_554_432
	maxImageWorkers       = 16

	// Keep legacy SVG bundling aligned with the raster pipeline's all-asset
	// ceilings. Fetched bytes, post-content-decoding source bytes, generated
	// data-URI bytes, and final SVG output are independent inclusive budgets.
	// The locator ceiling also matches imageasset's source limit.
	maxImageReferences                = 4_096
	maxImageReferenceBytes            = 64 << 10
	maxFetchedImageBytes        int64 = 512 << 20
	maxSourceImageBytes         int64 = 512 << 20
	maxBundledImageBytes        int64 = 512 << 20
	maxBundledOutputBytes       int64 = 512 << 20
	maxImageCacheEntries              = maxImageReferences
	maxImageCacheBytes          int64 = maxBundledImageBytes
	maxReportedImageFailures          = 8
	maxImageReferenceLabelBytes       = 256
	maxImageErrorLabelBytes           = 1_024
	maxBundleReadChunkBytes           = 32 << 10
)

var imgCache = newImageCache(maxImageCacheEntries, maxImageCacheBytes)

var imageRegex = regexp.MustCompile(`<image href="([^"]+)"`)

// BundleLocal bundles local image references using the deny-by-default local
// file policy. Call BundleLocalWithPolicy to deliberately permit local files.
func BundleLocal(ctx context.Context, l simplelog.Logger, inputPath string, in []byte, cacheImages bool) ([]byte, error) {
	return BundleLocalWithPolicy(ctx, l, inputPath, in, localfile.Policy{}, cacheImages)
}

// BundleLocalWithPolicy bundles local image references allowed by localFiles.
// Use localfile.Rooted for untrusted input. localfile.Unrestricted is intended
// only for trusted local applications such as the D2 command-line interface.
func BundleLocalWithPolicy(ctx context.Context, l simplelog.Logger, inputPath string, in []byte, localFiles localfile.Policy, cacheImages bool) ([]byte, error) {
	return bundle(ctx, l, inputPath, in, localFiles, false, cacheImages, netpolicy.Policy{})
}

func BundleRemote(ctx context.Context, l simplelog.Logger, in []byte, cacheImages bool) ([]byte, error) {
	return BundleRemoteWithPolicy(ctx, l, in, cacheImages, netpolicy.Policy{})
}

// BundleRemoteWithPolicy bundles HTTP(S) images under policy. The zero policy
// permits public destinations only; trusted callers may explicitly opt into
// private-network assets.
func BundleRemoteWithPolicy(ctx context.Context, l simplelog.Logger, in []byte, cacheImages bool, policy netpolicy.Policy) ([]byte, error) {
	return bundle(ctx, l, "", in, localfile.Policy{}, true, cacheImages, policy)
}

type repl struct {
	href string
	to   []byte
	err  error
}

type imageMatch struct {
	start     int
	end       int
	hrefStart int
	hrefEnd   int
}

type bundleLimitError struct {
	name   string
	actual int64
	limit  int64
}

func (e *bundleLimitError) Error() string {
	return fmt.Sprintf("%s exceeds maximum of %d bytes: %d", e.name, e.limit, e.actual)
}

type bundleBudget struct {
	mu               sync.Mutex
	fetchedBytes     int64
	sourceBytes      int64
	bundledBytes     int64
	maxFetchedBytes  int64
	maxSourceBytes   int64
	maxBundledBytes  int64
	exhaustedByError *bundleLimitError
}

func (b *bundleBudget) reserveFetched(bytes int64) error {
	return b.reserve("cumulative fetched image bytes", &b.fetchedBytes, b.maxFetchedBytes, bytes)
}

func (b *bundleBudget) reserveSource(bytes int64) error {
	return b.reserve("cumulative source image bytes", &b.sourceBytes, b.maxSourceBytes, bytes)
}

func (b *bundleBudget) reserveBundled(bytes int64) error {
	return b.reserve("cumulative bundled image bytes", &b.bundledBytes, b.maxBundledBytes, bytes)
}

func (b *bundleBudget) reserve(name string, used *int64, limit, bytes int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exhaustedByError != nil {
		return b.exhaustedByError
	}
	if bytes > limit-*used {
		b.exhaustedByError = &bundleLimitError{name: name, actual: *used + bytes, limit: limit}
		return b.exhaustedByError
	}
	*used += bytes
	return nil
}

func (b *bundleBudget) exhausted() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exhaustedByError == nil {
		return nil
	}
	return b.exhaustedByError
}

type bundleBudgetReader struct {
	ctx    context.Context
	reader io.Reader
	budget *bundleBudget
}

func (r *bundleBudgetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if err := r.budget.exhausted(); err != nil {
		return 0, err
	}
	if len(p) > maxBundleReadChunkBytes {
		p = p[:maxBundleReadChunkBytes]
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		if budgetErr := r.budget.reserveFetched(int64(n)); budgetErr != nil {
			return 0, budgetErr
		}
	}
	return n, err
}

func bundle(ctx context.Context, l simplelog.Logger, inputPath string, svg []byte, localFiles localfile.Policy, isRemote, cacheImages bool, policy netpolicy.Policy) (_ []byte, err error) {
	if isRemote {
		defer xdefer.Errorf(&err, "failed to bundle remote images")
	} else {
		defer xdefer.Errorf(&err, "failed to bundle local images")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()

	matches, hrefs, err := findImageElements(ctx, svg, isRemote)
	if err != nil {
		return svg, err
	}

	if len(hrefs) == 0 {
		return svg, nil
	}
	var client *http.Client
	if isRemote {
		client, err = netpolicy.NewHTTPClient(httpClient, policy)
		if err != nil {
			return svg, err
		}
	}

	budget := &bundleBudget{
		maxFetchedBytes: maxFetchedImageBytes,
		maxSourceBytes:  maxSourceImageBytes,
		maxBundledBytes: maxBundledImageBytes,
	}
	return runWorkers(ctx, l, inputPath, svg, matches, hrefs, localFiles, isRemote, cacheImages, client, policy, budget)
}

// findImageElements records every eligible occurrence, but returns each href
// only once for fetching. Iterative matching avoids allocating an unbounded
// regexp result before the reference ceiling can be enforced.
func findImageElements(ctx context.Context, svg []byte, isRemote bool) ([]imageMatch, []string, error) {
	var matches []imageMatch
	var hrefs []string
	unique := make(map[string]struct{})
	for offset := 0; offset < len(svg); {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		indices := imageRegex.FindSubmatchIndex(svg[offset:])
		if indices == nil {
			break
		}
		match := imageMatch{
			start:     offset + indices[0],
			end:       offset + indices[1],
			hrefStart: offset + indices[2],
			hrefEnd:   offset + indices[3],
		}
		offset = match.end
		hrefBytes := svg[match.hrefStart:match.hrefEnd]

		// Skip already bundled images before applying the locator limit: a valid
		// data URI may be larger than the source image it contains.
		if bytes.HasPrefix(hrefBytes, []byte("data:")) {
			continue
		}
		href := string(hrefBytes)

		u, err := url.Parse(html.UnescapeString(href))
		isRemoteImage := err == nil && strings.HasPrefix(u.Scheme, "http")
		if isRemoteImage != isRemote {
			continue
		}
		if len(hrefBytes) > maxImageReferenceBytes {
			return nil, nil, &bundleLimitError{name: "image reference", actual: int64(len(hrefBytes)), limit: maxImageReferenceBytes}
		}
		if len(matches) == maxImageReferences {
			return nil, nil, fmt.Errorf("image references exceed maximum of %d", maxImageReferences)
		}
		matches = append(matches, match)
		if _, ok := unique[href]; !ok {
			unique[href] = struct{}{}
			hrefs = append(hrefs, href)
		}
	}
	return matches, hrefs, nil
}

func runWorkers(ctx context.Context, l simplelog.Logger, inputPath string, svg []byte, matches []imageMatch, hrefs []string, localFiles localfile.Policy, isRemote, cacheImages bool, client *http.Client, policy netpolicy.Policy, budget *bundleBudget) (_ []byte, err error) {
	var wg sync.WaitGroup
	replc := make(chan repl)

	wg.Add(len(hrefs))
	go func() {
		wg.Wait()
		close(replc)
	}()

	sema := make(chan struct{}, maxImageWorkers)
	var errhrefs []string
	var failedCount int
	var limitErr error
	replacements := make(map[string][]byte, len(hrefs))

	// Start workers as the sema allows.
	go func() {
		for i, href := range hrefs {
			select {
			case sema <- struct{}{}:
			case <-ctx.Done():
				for range hrefs[i:] {
					wg.Done()
				}
				return
			}
			if budget.exhausted() != nil {
				<-sema
				for range hrefs[i:] {
					wg.Done()
				}
				return
			}
			go func() {
				defer func() {
					wg.Done()
					<-sema
				}()

				bundledImage, err := worker(ctx, l, inputPath, href, localFiles, isRemote, cacheImages, client, policy, budget)
				if err != nil {
					l.Error(fmt.Sprintf("failed to bundle %s: %s",
						boundedLabel(href, maxImageReferenceLabelBytes),
						boundedLabel(err.Error(), maxImageErrorLabelBytes),
					))
				}
				select {
				case <-ctx.Done():
				case replc <- repl{
					href: href,
					to:   bundledImage,
					err:  err,
				}:
				}
			}()
		}
	}()

	t := time.NewTicker(time.Second * 5)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return svg, fmt.Errorf("failed to wait for workers: %w", ctx.Err())
		case <-t.C:
			l.Info("fetching images...")
		case repl, ok := <-replc:
			if !ok {
				output, err := applyReplacements(ctx, svg, matches, replacements, maxBundledOutputBytes)
				if err != nil {
					return svg, errors.Join(err, limitErr)
				}
				if len(errhrefs) > 0 {
					failureErr := fmt.Errorf("%v", errhrefs)
					if failedCount > len(errhrefs) {
						failureErr = fmt.Errorf("%v (and %d more)", errhrefs, failedCount-len(errhrefs))
					}
					return output, errors.Join(failureErr, limitErr)
				}
				return output, nil
			}
			if repl.err != nil {
				failedCount++
				if len(errhrefs) < maxReportedImageFailures {
					errhrefs = append(errhrefs, boundedLabel(repl.href, maxImageReferenceLabelBytes))
				}
				var bundleLimit *bundleLimitError
				if limitErr == nil && errors.As(repl.err, &bundleLimit) {
					limitErr = bundleLimit
				}
				continue
			}
			replacements[repl.href] = repl.to
		}
	}
}

func boundedLabel(label string, maxBytes int) string {
	if len(label) <= maxBytes {
		return label
	}
	return label[:maxBytes-3] + "..."
}

func applyReplacements(ctx context.Context, svg []byte, matches []imageMatch, replacements map[string][]byte, maxOutputBytes int64) ([]byte, error) {
	if len(replacements) == 0 {
		return svg, nil
	}
	outputBytes := int64(len(svg))
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		replacement, ok := replacements[string(svg[match.hrefStart:match.hrefEnd])]
		if !ok {
			continue
		}
		outputBytes += int64(len(replacement)) - int64(match.end-match.start)
		if outputBytes > maxOutputBytes {
			return nil, &bundleLimitError{name: "bundled SVG output", actual: outputBytes, limit: maxOutputBytes}
		}
	}
	if outputBytes > maxOutputBytes {
		return nil, &bundleLimitError{name: "bundled SVG output", actual: outputBytes, limit: maxOutputBytes}
	}

	output := make([]byte, 0, int(outputBytes))
	previous := 0
	for _, match := range matches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		replacement, ok := replacements[string(svg[match.hrefStart:match.hrefEnd])]
		if !ok {
			continue
		}
		output = append(output, svg[previous:match.start]...)
		output = append(output, replacement...)
		previous = match.end
	}
	output = append(output, svg[previous:]...)
	return output, nil
}

type imageCacheKey struct {
	href                 string
	isRemote             bool
	allowPrivateNetworks bool
	localPolicyKey       string
}

func worker(ctx context.Context, l simplelog.Logger, inputPath string, href string, localFiles localfile.Policy, isRemote, cacheImages bool, client *http.Client, policy netpolicy.Policy, budget *bundleBudget) ([]byte, error) {
	if err := budget.exhausted(); err != nil {
		return nil, err
	}
	cacheKey := imageCacheKey{href: href, isRemote: isRemote, allowPrivateNetworks: policy.AllowPrivateNetworks}
	hrefLabel := boundedLabel(href, maxImageReferenceLabelBytes)
	localPath := ""
	if !isRemote {
		localPath = html.UnescapeString(href)
		if inputPath != "-" && !filepath.IsAbs(localPath) {
			localPath = filepath.Join(filepath.Dir(inputPath), localPath)
		}
		policyKey, err := localFiles.CacheKey(localPath)
		if err != nil {
			return nil, err
		}
		cacheKey.localPolicyKey = policyKey
	}
	if cacheImages {
		if hit, ok := imgCache.Load(cacheKey); ok {
			if err := budget.reserveBundled(int64(len(hit))); err != nil {
				return nil, err
			}
			return hit, nil
		}
	}
	var buf []byte
	var mimeType string
	var err error
	if isRemote {
		l.Debug(fmt.Sprintf("fetching %s remotely", hrefLabel))
		buf, mimeType, err = httpGet(ctx, l, client, html.UnescapeString(href), budget)
	} else {
		l.Debug(fmt.Sprintf("reading %s from disk", hrefLabel))
		buf, err = readLocal(ctx, localFiles, localPath, budget)
	}
	if err != nil {
		return nil, err
	}
	if err := budget.reserveSource(int64(len(buf))); err != nil {
		return nil, err
	}

	declaredMIMEType := mimeType
	mimeType, ok := canonicalImageMIMEType(declaredMIMEType)
	if !ok {
		mimeType, ok = sniffImageMIMEType([]byte(href), buf, isRemote)
		if !ok {
			mimeType = "application/octet-stream"
		}
		l.Debug(fmt.Sprintf("invalid or unsupported mimetype %q - sniffed MIME type for %s: %s", declaredMIMEType, hrefLabel, mimeType))
	} else {
		l.Debug(fmt.Sprintf("mimetype provided for %s: %s", hrefLabel, mimeType))
	}
	prefix := `<image href="data:` + svg.EscapeText(mimeType) + `;base64,`
	encodedBytes := base64.StdEncoding.EncodedLen(len(buf))
	outputBytes := len(prefix) + encodedBytes + 1
	if err := budget.reserveBundled(int64(outputBytes)); err != nil {
		return nil, err
	}
	out := make([]byte, outputBytes)
	copy(out, prefix)
	base64.StdEncoding.Encode(out[len(prefix):len(prefix)+encodedBytes], buf)
	out[len(out)-1] = '"'
	if cacheImages {
		imgCache.Store(cacheKey, out)
	}
	return out, nil
}

func readLocal(ctx context.Context, localFiles localfile.Policy, filePath string, budget *bundleBudget) (_ []byte, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := localFiles.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close local image %q: %w", filePath, closeErr))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat local image %q: %w", filePath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("local image %q is not a regular file", filePath)
	}
	if info.Size() > maxImageSize {
		return nil, fmt.Errorf("local image exceeds maximum size of %d bytes", maxImageSize)
	}
	buf, err := io.ReadAll(&bundleBudgetReader{ctx: ctx, reader: io.LimitReader(file, maxImageSize+1), budget: budget})
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxImageSize {
		return nil, fmt.Errorf("local image exceeds maximum size of %d bytes", maxImageSize)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return buf, nil
}

var httpClient = &http.Client{}

func httpGet(ctx context.Context, l simplelog.Logger, client *http.Client, href string, budget *bundleBudget) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	hrefLabel := boundedLabel(href, maxImageReferenceLabelBytes)

	req, err := http.NewRequestWithContext(ctx, "GET", href, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("DNT", "1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "image")
	req.Header.Set("Sec-Fetch-Mode", "no-cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	l.Debug(fmt.Sprintf("fetched %s remotely - response code %v", hrefLabel, resp.StatusCode))
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("expected status 200 but got %d %s", resp.StatusCode, resp.Status)
	}
	r := http.MaxBytesReader(nil, resp.Body, maxImageSize)
	buf, err := io.ReadAll(&bundleBudgetReader{ctx: ctx, reader: r, budget: budget})
	if err != nil {
		return nil, "", err
	}
	contentType := resp.Header.Get("Content-Type")
	contentEncoding := resp.Header.Get("Content-Encoding")
	if contentEncoding != "" {
		buf, err = decodeContentEncoding(buf, contentEncoding)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode %q response for %s: %w", contentEncoding, hrefLabel, err)
		}
	}
	l.Debug(fmt.Sprintf("fetched content type: %s, Content length: %d bytes", contentType, len(buf)))

	return buf, contentType, nil
}

func decodeContentEncoding(buf []byte, contentEncoding string) ([]byte, error) {
	encodings := strings.Split(contentEncoding, ",")
	for i := len(encodings) - 1; i >= 0; i-- {
		encoding := strings.TrimSpace(strings.ToLower(encodings[i]))
		if encoding == "" || encoding == "identity" {
			continue
		}

		var err error
		switch encoding {
		case "gzip", "x-gzip":
			buf, err = gunzip(buf)
		case "br":
			buf, err = readDecoded(brotli.NewReader(bytes.NewReader(buf)))
		case "deflate":
			buf, err = inflate(buf)
		default:
			return nil, fmt.Errorf("unsupported content encoding %q", encoding)
		}
		if err != nil {
			return nil, err
		}
	}
	return buf, nil
}

func gunzip(buf []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readDecoded(r)
}

func inflate(buf []byte) ([]byte, error) {
	if zr, err := zlib.NewReader(bytes.NewReader(buf)); err == nil {
		defer zr.Close()
		return readDecoded(zr)
	}
	fr := flate.NewReader(bytes.NewReader(buf))
	defer fr.Close()
	return readDecoded(fr)
}

func readDecoded(r io.Reader) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, maxImageSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxImageSize {
		return nil, fmt.Errorf("decoded image exceeds maximum size of %d bytes", maxImageSize)
	}
	return buf, nil
}

// canonicalImageMIMEType strictly parses a declared media type and returns a
// parameter-free image media type suitable for a data URI. Preserve arbitrary
// valid image subtypes for browser compatibility while normalizing aliases D2
// has historically supported.
func canonicalImageMIMEType(value string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "", false
	}
	mediaType = strings.ToLower(mediaType)
	switch mediaType {
	case "image/jpg", "image/pjpeg":
		return "image/jpeg", true
	case "image/x-png":
		return "image/png", true
	case "text/xml", "application/xml", "application/svg+xml":
		return "image/svg+xml", true
	default:
		return mediaType, mediaType != "image/*" && strings.HasPrefix(mediaType, "image/")
	}
}

// sniffImageMIMEType sniffs the MIME type of href based on its file extension
// and contents, accepting only values canonicalImageMIMEType can make safe.
func sniffImageMIMEType(href, buf []byte, isRemote bool) (string, bool) {
	p := string(href)
	if isRemote {
		u, err := url.Parse(html.UnescapeString(p))
		if err != nil {
			p = ""
		} else {
			p = u.Path
		}
	}
	for _, candidate := range []string{
		mime.TypeByExtension(path.Ext(p)),
		http.DetectContentType(buf),
	} {
		if mimeType, ok := canonicalImageMIMEType(candidate); ok {
			return mimeType, true
		}
	}
	if bytes.Contains(buf, []byte("<svg")) {
		return "image/svg+xml", true
	}
	return "", false
}
