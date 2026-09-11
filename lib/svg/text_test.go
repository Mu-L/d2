package svg

import "testing"

func TestEscapeAttribute(t *testing.T) {
	t.Parallel()

	const value = `url('#gradient') & " < > '`
	const want = `url('#gradient') &amp; &#34; &lt; &gt; '`
	if got := EscapeAttribute(value); got != want {
		t.Fatalf("EscapeAttribute(%q) = %q, want %q", value, got, want)
	}
}
