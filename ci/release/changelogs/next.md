#### Features 🚀

#### Improvements 🧹

- Remove the `d2plugin` package and executable layout-plugin protocol,
  including executable discovery, plugin render post-processing, and the standalone
  `d2plugin-dagre` command. The CLI supports the bundled `dagre`, `elk`,
  and `tala` engines and their existing options. Use `d2 --layout=<name>` to select
  an engine. Go integrations should use the packages under `d2layouts` and the
  layout/router resolvers in `d2lib.CompileOptions`. The deprecated
  `d2layouts/d2layoutfeatures` package is also removed. `d2cli.LayoutResolver`
  and `d2cli.RouterResolver` now take only `(ctx, ms)`, without a plugin list.
  Apply custom output transformations in the calling application after rendering.
- Centralize escaped SVG attribute and text serialization, encode unsafe local
  IDs, and keep deprecated raw element fields only as checked compatibility boundaries.
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
