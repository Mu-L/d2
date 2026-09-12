#### Features 🚀

#### Improvements 🧹

- d2svg:
  - Prevent malformed diagram or raw render values from breaking out of SVG text
    and attributes. [#2919](https://github.com/d2lang/d2/pull/2919)
  - Encode unsafe local IDs consistently so definitions and references continue
    to match. [#2919](https://github.com/d2lang/d2/pull/2919)
- assets:
  - Apply the same local/network access policy and cumulative resource budgets to
    SVG and raster exports. [#2918](https://github.com/d2lang/d2/pull/2918)
  - Accept only valid PNG, JPEG, GIF, WebP, and SVG resources during image
    bundling; malformed data and other formats now fail instead of being embedded
    based only on a response `Content-Type`. [#2918](https://github.com/d2lang/d2/pull/2918)
  - Block private and reserved network destinations by default; trusted fetching
    requires `--allow-private-network` or `D2_ALLOW_PRIVATE_NETWORK`. [#2899](https://github.com/d2lang/d2/pull/2899)
  - Require Go API callers to choose an explicit rooted or trusted unrestricted
    policy for local imports and image assets. CLI and watch retain trusted
    local-file access. [#2901](https://github.com/d2lang/d2/pull/2901)
- plugins: discover external layout-plugin binaries without running them, and
  execute only an unambiguously selected external engine. [#2897](https://github.com/d2lang/d2/pull/2897)
- watch: authenticate HTTP and WebSocket access with a fresh per-process
  capability, exchanged for a token-free browser session. [#2910](https://github.com/d2lang/d2/pull/2910)
- exports:
  - Map unsafe board names to bounded portable output paths. [#2900](https://github.com/d2lang/d2/pull/2900)
  - Publish multiboard output transactionally within the selected output root. [#2900](https://github.com/d2lang/d2/pull/2900)
  - Preserve unrelated or user-modified files when generated output is updated. [#2900](https://github.com/d2lang/d2/pull/2900)
  - Prevent concurrent local path replacement from redirecting publication
    outside the selected tree. [#2908](https://github.com/d2lang/d2/pull/2908)

#### Bugfixes ⛑️

- compiler:
  - Limit combined map/array nesting to 128. [#2894](https://github.com/d2lang/d2/pull/2894)
  - Honor cancellation during parsing and IR compilation, and prevent `d2 fmt`
    from writing output after cancellation. [#2894](https://github.com/d2lang/d2/pull/2894)
  - Limit active import chains to 128. [#2904](https://github.com/d2lang/d2/pull/2904)
  - Reject cyclic composite variables with source diagnostics instead of allowing
    unbounded recursion. [#2907](https://github.com/d2lang/d2/pull/2907)
- decoding and assets:
  - Cap decompressed URL-encoded D2 input at 16 MiB. [#2902](https://github.com/d2lang/d2/pull/2902)
  - Bound image references, locators, fetched and decoded bytes, cached data, and
    final output during SVG image bundling. [#2911](https://github.com/d2lang/d2/pull/2911)
- rendering:
  - Reject malformed, cyclic, reused, or nil raw diagram board graphs with
    ordinary errors instead of panics or fatal recursion. [#2912](https://github.com/d2lang/d2/pull/2912)
  - Validate raw geometry, routes, arrows, fonts, and arithmetic before recursive
    rendering. [#2912](https://github.com/d2lang/d2/pull/2912)
  - Limit raw graphs to 1,024 levels and 4,096 boards. [#2912](https://github.com/d2lang/d2/pull/2912)
- links and paints:
  - Reject dangerous ordinary link schemes after decoding common obfuscation. [#2896](https://github.com/d2lang/d2/pull/2896)
  - Require gradient stop positions to be finite numbers or percentages. [#2905](https://github.com/d2lang/d2/pull/2905)
  - Reject external SVG paint URLs and unsafe fill-pattern or theme values in raw
    render targets. [#2916](https://github.com/d2lang/d2/pull/2916)
  - Apply the dangerous-link policy to legend shapes. [#2917](https://github.com/d2lang/d2/pull/2917)
- d2svg:
  - Escape custom shape and connection classes. [#2895](https://github.com/d2lang/d2/pull/2895)
  - Sanitize bundled-image media types and escape serialized image URLs. [#2898](https://github.com/d2lang/d2/pull/2898)
  - Escape SQL-table constraint text. [#2903](https://github.com/d2lang/d2/pull/2903)
  - Encode and escape clip-path identifiers consistently in definitions and
    references. [#2906](https://github.com/d2lang/d2/pull/2906)
  - Escape color attributes across shapes, connections, labels, and themes. [#2909](https://github.com/d2lang/d2/pull/2909)

---

For the latest d2.js changes, see separate [changelog](https://github.com/d2lang/d2/blob/master/d2js/js/CHANGELOG.md).
