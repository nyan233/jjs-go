# Reusing map iterators in generated encoders

Source review: 2026-11-23. Baseline: `3896491`. This note establishes a
compatibility strategy, not a measured performance result. No implementation
or benchmark was changed as part of this review.

## Recommended boundary

Generate the JSON traversal loop and key/value encoders from their static Go
types, but let the Go toolchain's iterator implementation advance the map.
Use a real `reflect.MapIter` stored in reusable, typed Go memory. Its public
`Reset` and `Next` methods select the appropriate runtime implementation.
Read only the current key/element pointers through checked field offsets;
do not reproduce buckets, Swiss groups, probing, growth or randomized order.

This is an engineering recommendation. The raw-pointer access is an explicit
dependency on private layout, not a public reflect API. Keep it isolated and
validate the small part of the layout actually used. A public-API fallback
can use reusable typed destinations with `SetIterKey`/`SetIterValue`, which
avoid constructing a new result Value for every entry.
[Reflect API documentation](https://pkg.go.dev/reflect#Value.SetIterValue)
describes that alternative.

## Why the current linkname path allocates

The Go 1.25 legacy `runtime.mapiterinit` is a compatibility shim for callers
using the old iterator shape. Every initialization allocates a separate
`maps.Iter`, then exposes its current key and element through the legacy
structure. Reusing the caller's old structure alone therefore does not remove
this allocation. Adding `noescape` to the old declaration would not remove
the allocation inside the runtime either. See the
[Go 1.25 compatibility shim](https://github.com/golang/go/blob/go1.25.0/src/runtime/linkname_swiss.go#L19-L99).

In contrast, the Swiss reflect implementation embeds the actual iterator
inside `MapIter`. Its `Next` calls initialization/advancement on that embedded
object; `Reset` clears it for reuse. There is no `new(maps.Iter)` on this
route. `Key` and `Value` use `copyVal`, so using those accessors can introduce
per-entry copying/allocation instead of solving the original problem.
[Go 1.25 reflect implementation](https://github.com/golang/go/blob/go1.25.0/src/reflect/map_swiss.go#L170-L379)
shows both paths.

This supports an expectation of no per-map iterator allocation after the
context has enough reusable state. It does not imply zero allocations for
first use, deeper nesting, pool misses, output growth or custom marshalers;
those require measurement.

## Version observations

The following are source observations for the named tags, not a promise
about future Go internals:

| Versions/configuration | `MapIter` representation | Relevant distinction |
|---|---|---|
| Go 1.21–1.23 | `reflect.Value` followed by embedded old `hiter` | Old iterator's first two fields are key/element pointers. |
| Go 1.24–1.25, Swiss | `reflect.Value` followed by embedded `maps.Iter` | First two fields remain key/element pointers; later layout and pointer map differ from old `hiter`. |
| Go 1.24–1.25, `GOEXPERIMENT=noswissmap` | `reflect.Value` followed by old `hiter` | This old-map iterator additionally has `clearSeq`; do not assume the Go 1.21 total size. |
| Go 1.26 | Swiss iterator in `reflect/map.go` | Uses `abi.MapType`; the old Swiss experiment flag and alternate implementation are gone. |
| Go 1.27 | Same `MapIter` and key/element prefix | The map implementation additionally supports `MapSplitGroup`; raw group scanning would need to account for split and interleaved layouts. |

Sources: [Go 1.21 reflect](https://github.com/golang/go/blob/go1.21.0/src/reflect/value.go),
[Go 1.22 reflect](https://github.com/golang/go/blob/go1.22.0/src/reflect/value.go),
[Go 1.23 reflect](https://github.com/golang/go/blob/go1.23.0/src/reflect/value.go),
[Go 1.24 Swiss reflect](https://github.com/golang/go/blob/go1.24.0/src/reflect/map_swiss.go),
[Go 1.25 non-Swiss reflect](https://github.com/golang/go/blob/go1.25.0/src/reflect/map_noswiss.go),
[Go 1.26 reflect](https://github.com/golang/go/blob/go1.26.0/src/reflect/map.go),
[Go 1.26 experiment flags](https://github.com/golang/go/blob/go1.26.0/src/internal/goexperiment/flags.go),
and [Go 1.27 reflect](https://github.com/golang/go/blob/go1.27.0/src/reflect/map.go).

For the reviewed 64-bit layouts, the iterator begins after the 24-byte
`reflect.Value`; key and element are at relative offsets 0 and 8. Prefer
deriving these offsets once with `reflect.TypeOf(reflect.MapIter{})`, its
`hiter` field and that field's `key`/`elem` metadata. Validate names, kinds,
sizes and bounds before enabling raw loads. This avoids depending on the
remaining iterator's total size. Go's compiler itself requires the first two
Swiss fields to hold key/element pointers, as documented in
[Go 1.27 `maps.Iter`](https://github.com/golang/go/blob/go1.27.0/src/internal/runtime/maps/table.go#L663-L695).

The GC distinction matters even where total sizes happen to match. Old
`hiter` has eight leading pointer fields. Swiss `Iter` has four leading
pointer fields, scalar offsets/counters, then table/group pointers. Using a
real `MapIter` lets the compiler provide the correct pointer bitmap for the
selected Go version. A `[N]uintptr` scratch area or old-layout struct is not
an equivalent GC root. The JIT only needs a pointer to the typed state in its
own stack map, rather than a hand-maintained bitmap for the whole iterator.
Compare the [Go 1.21 old iterator](https://github.com/golang/go/blob/go1.21.0/src/reflect/value.go)
with [Go 1.25 Swiss iterator](https://github.com/golang/go/blob/go1.25.0/src/internal/runtime/maps/table.go).

## Linkname is not a shortcut to the new API

`runtime.mapIterStart`, `runtime.mapIterNext`, and their reflect equivalents
are internal functions without a pushed `go:linkname` declaration in the
reviewed Swiss source. They are not an officially exposed external API.
Starting with Go 1.23, the linker rejects new references to unexposed standard
library internals, including assembly references. Do not require callers to
disable that check. Sources: [runtime map iteration](https://github.com/golang/go/blob/go1.25.0/src/runtime/map_swiss.go#L156-L179)
and [Go 1.23 linker changes](https://go.dev/doc/go1.23#linker).

Calling the exported `(*reflect.MapIter).Next` method through a normal typed
Go helper or a correctly described JIT call avoids that linkname dependency.
It also keeps the runtime's advancement, map-growth handling and concurrent
write checks. This does not make concurrent mutation legal or synchronize
map access. The relevant checks remain in
[`maps.Iter.Next`](https://github.com/golang/go/blob/go1.25.0/src/internal/runtime/maps/table.go).

## Integration requirements

These are implementation recommendations derived from the above:

1. Obtain the map Value from its existing, live field/header address, for
   example via `reflect.NewAt(mapType, mapFieldPtr).Elem()`. Copying the map
   pointer into a new local header and retaining that local's address can
   reintroduce one escaping allocation per map.
2. Give every simultaneously active map its own iterator state. Use stable
   addresses, such as pooled nodes or blocks; nested encoding must not move
   or overwrite the parent's active iterator during storage growth.
3. Clear used iterators with `Reset(reflect.Value{})` on success, error and
   panic cleanup. The documented reset behavior releases the iterator's map
   reference. Retain reusable storage, not the previous maps.
   [Reset documentation](https://pkg.go.dev/reflect#MapIter.Reset).
4. Keep the current key, element, map state and parent data reachable across
   Go calls and stack movement. Perform pointer-bearing state writes in Go
   so its write barriers apply. Raw generated code should read state, not
   fabricate a runtime iterator or write its internal pointers.
5. Generate integer key width/sign choices and string key quoting once per
   map type. Preserve map-value non-addressability when choosing custom
   marshaler methods. Preserve the existing key ordering contract.
6. A fully public fallback can cache writable key/value reflection objects
   for each active type/depth and fill them using `SetIterKey`/`SetIterValue`.
   It copies payloads but needs no private map/iterator layout. If layout
   validation fails on a future Go version, choose that fallback or report
   an explicit unsupported configuration instead of guessing offsets.

Required validation should cover both map implementations where available,
nil/empty maps, nesting, indirect large values, all supported key widths,
named types, custom marshalers, errors, forced GC/stack growth and repeated
context reuse. Measure allocation counts separately for first use and warmed
reuse. Public encoding results must also be compared semantically because
the runtime deliberately randomizes map iteration order.

## Adjacent compatibility finding

Go 1.26/1.27 map type construction sets `abi.TFlagDirectIface`, while this
repository's baseline `IsDirectInterfaceType` checks the earlier `Kind` bit.
That is a concrete lead for the already-reproduced Go 1.26 direct-interface
wrapper issue; this research does not change or validate a fix for it.
[Go 1.26 `MapOf`](https://github.com/golang/go/blob/go1.26.0/src/reflect/map.go#L50-L58).
