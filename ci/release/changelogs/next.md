#### Features 🚀

#### Improvements 🧹

- SVG image bundling now uses the same document-scoped resolver as raster
  rendering, with shared local/network policy, caching, and cumulative resource
  budgets. Bundled images are strictly limited to valid PNG, JPEG, GIF, WebP,
  and SVG resources; malformed data and other image formats now fail bundling
  instead of being embedded from an arbitrary response `Content-Type`. The
  deprecated `imgbundler` cache flag is now scoped to one invocation; callers
  that need reuse across documents should share an explicit `imageasset` cache.

#### Bugfixes ⛑️

---

For the latest d2.js changes, see separate [changelog](https://github.com/d2lang/d2/blob/master/d2js/js/CHANGELOG.md).
