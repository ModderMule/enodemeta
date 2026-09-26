# enodemeta: the shared library

`enodemeta` is the Go module holding everything four codebases have to agree on:
the torrent crawler, the Usenet crawler, eNode-go, and eMuleQt. It used to live
inside torrent-crawler as `pkg/enodemeta` and now has its own repository,
`github.com/ModderMule/enodemeta`; the module path did not change, so no import
did either.

How each consumer reaches it:

| Consumer | How |
|---|---|
| torrent-crawler, usenet-crawler | `replace github.com/ModderMule/enodemeta => ../enodemeta` — cloned side by side |
| eNode-go | git submodule at `./enodemeta`, `replace ... => ./enodemeta` |
| eMuleQt | generates C++ from `proto/` and ports `metahash`/`btid` by hand |

Its only dependencies are `google.golang.org/protobuf` and
`connectrpc.com/connect/v2`. Nothing in it imports the crawler.

## What is in it

| Package | What it holds |
|---|---|
| `proto/`, `gen/` | The `.proto` files and the generated protobuf + connect code |
| `metahash/` | The eD2K pseudo-hash: minting, parsing, cross-checking, verification |
| `btid/` | The `bt:v1:`/`bt:v2:` id namespace, which is also the `catalog_id` format |
| `model/` | Domain types with no protobuf imports |
| `pbconv/` | Conversions between `model` and the generated types |
| `tags/` | The `FT_META_*` tag ids, capability bits, flags, `MaxSources` |
| `bencode/` | A decoder that keeps raw spans, and an encoder |
| `torrentmeta/` | Torrent identity: v1, v2 and hybrid parsing, file trees, padding |
| `nzbmeta/` | NZB identity: parsing, the canonical digest, a deterministic writer, subjects, PAR2, shortfall |
| `magnet/` | Magnet construction, including BEP 53 `so=` |
| `filetype/` | Extension to eD2K file-type string |
| `verify.go` | `VerifyMetaFile`: recompute the identity and check it against a hash |
| `testdata/` | The vectors a C++ port is checked against |

## The rule that shapes it

**No generated protobuf type leaves the transport layer.** `model.Entry` is what
the rest of a program passes around; `pbconv` converts at the edge. The moment a
`*metav1.MetaEntry` appears inside a catalogue or a storage engine, adding a
second representation stops being an adapter and becomes a rewrite.

`gen/` is checked in. Regenerate with `scripts/gen-proto.sh`, which installs
pinned plugins into `bin/tools` rather than using whatever is on `$PATH`.

## The eD2K pseudo-hash

An eD2K search result is identified by a 16-byte hash. A torrent has no such
hash, so one is minted — a *pseudo*-hash, carrying enough structure that a
client can recognise it as a catalogue row rather than a file it could download.

### Layout

```
 0    1    2    3       4    5    6 .. 15
+----+----+----+-------+---------+-------------------+
| ED | 2B | 01 | K | F | idx LE  | 10-byte digest    |
+----+----+----+-------+---------+-------------------+
  magic    ver  kind/flags  uint16   XOR fold of the identity
```

- **Magic** `0xED 0x2B` and **version** `0x01`. An unknown version is a distinct
  error, because a client must drop those silently rather than guess.
- **Kind** (high nibble): 1 = BitTorrent v1 or hybrid (20-byte infohash),
  2 = BitTorrent v2 only (32-byte), 3 = NZB (32-byte canonical digest).
- **Flags** (low nibble): `0x1` multi-file set, `0x2` path authoritative,
  `0x4` protected, `0x8` reserved and rejected when set.
- **File index** as uint16 little-endian. `0xFFFF` is the whole-set row.
- **Digest**: `Fold10` of the release identity — bytes 6..15.

### The fold

`Fold(d, n)` XOR-folds a digest of any length down to `n` bytes:
`out[i mod n] ^= d[i]`. For a 20-byte SHA-1 folded to 10 it is "XOR the two
halves"; for a 32-byte SHA-256 it is three passes, the last partial. Folding a
cryptographic digest preserves uniformity, because each output byte is the XOR
of independent uniform bytes, so the result behaves as a random 10-byte tag.

**The digest always covers the whole release, never the selected file.** That is
what makes `SameRelease(a, b)` — comparing bytes 6..15 — group every row of one
release, so a client can show a season pack's episodes together.

### Minting

`metahash.Mint(MintInput)` is the only way to build one, and `model.Entry.Mint`
is the only way a row gets one. The rules it enforces:

- The whole-set row uses `0xFFFF`.
- Above 65,535 files the hash keeps the low 16 bits, `FT_META_FILEINDEX` carries
  the truth, and flag `0x2` is set to say so.
- An entry whose `uint16(index)` is `0xFFFF` but which is not the whole-set row
  is refused: minting it would produce two rows with one hash, and they would
  collapse into one result in the client.

### Verification and guards

- `enodemeta.VerifyMetaFile(hash, bytes)` recomputes the identity from the bytes
  and compares bytes 6..15. It is the crawler's post-fetch self-check and the
  server's check before caching.
- `CrossCheck(hash, kind, version, flags, index)` compares a parsed hash against
  the `FT_META_*` tags that accompanied it. **The tags are authoritative**; the
  hash marker is only a cross-check, and a mismatch means drop the row.
- `IsMetaHash(b)` is the marker heuristic and `RejectOfferedFile(hash)` is the
  `OP_OFFERFILES` guard.

### The four nevers

A meta hash is not a file hash. It must **never** be:

1. published to Kad,
2. offered to a server in `OP_OFFERFILES`,
3. written into `known.met`,
4. used as a transfer id.

Doing any of them advertises a file that does not exist to a network that will
then ask for it.

### Vectors

`testdata/meta-hash-vectors.json` covers every kind and flag combination, a
wrong magic, an unknown version, a set reserved flag, a truncated hash and the
whole-set index. Regenerate with `go test ./metahash -update`; a C++ port is
expected to reproduce it exactly.

`testdata/fileindex-vectors.json` pins `file_index` against libtorrent's
numbering for v1 with BEP 47 pads, BitComet pads, hybrid, v2-only and NZB, with
every row's meta hash beside it. Regenerate with `go test . -update`.

`testdata/nzb-identity-vectors.json` pins the canonical NZB digest. Regenerate
with `go test ./nzbmeta -update`.

## Torrent identity

`torrentmeta.ParseInfo` takes the raw info dictionary — the exact bytes the
infohash covers — and returns name, files, sizes, and the v1/v2 hashes. The
things worth knowing:

- **The raw bytes matter.** `bencode` keeps a raw span for every value, because
  an infohash is the hash of the file's own bytes and a re-encode is not
  guaranteed to reproduce them.
- **Hybrids are checked.** A torrent carrying both a v1 file list and a v2 file
  tree must have them agree; one that does not is refused rather than
  catalogued under a hash that describes different contents.
- **Padding is excluded.** Both BEP 47's `attr=p` and BitComet's
  `_____padding_file_` convention. A padding file is an artefact of piece
  alignment, and advertising one is advertising 16 KiB of zeroes.
- **Indices are the torrent's own.** A file's index counts padding files, so a
  selected file's index has gaps. It has to: it is what a BEP 53 `so=` and a
  meta row both refer to.
- **Names are repaired for display, and flagged.** A path that was not valid
  UTF-8 is repaired with `strings.ToValidUTF8` and marked not authoritative,
  because another client repairs the same bytes differently and would look for a
  file that does not exist.
- **`V2Key`** is the first 20 bytes of the v2 infohash: what a v2 torrent is
  announced under on the DHT, and therefore what a sighting carries The
  whole v1/v2/hybrid picture is [docs/infohash.md](infohash.md).

## NZB identity

`nzbmeta.Parse` takes an NZB document and `nzbmeta.Identity` digests it. The
package mirrors `torrentmeta`'s conventions — package-level `Max*` bounds,
distinguishable sentinel errors, describe-or-refuse, repair-and-flag — and imports
only `metahash` plus the standard library, so the module's dependency budget is
untouched.

The digest is **not** over the bytes. Hashing the XML would make a re-generated
document with different whitespace, a different generator comment or a reordered
`<head>` into a different release, so the input is the one thing that actually
names the content on Usenet, the article message-ids:

```
identity_bytes := "nzb1\n"
for each <file> in document order:
    for each <segment> sorted ascending by @number (stable):
        identity_bytes += decimal(@number) + ":" + <segment text> + "\n"
digest := SHA-256(identity_bytes)
```

The eleven rules are in `nzbmeta/identity.go`. Four are worth knowing here:

- **A segment with an empty message-id emits no line.** This is the load-bearing
  one. It makes a `<file>` with no usable segment contribute nothing, so a reader
  that drops such a file (eMuleQt does, and so does this package) and one that
  keeps it produce the same digest — which removes the only real disagreement two
  conforming parsers had.
- **`<head>`, `<groups>`, `@poster`, `@date`, `@subject` and `@bytes` are all
  excluded.** So a repost of the same articles under another group and subject is
  deliberately the *same* release, and a daemon may serve a password-redacted copy
  that still passes the client's fold check.
- **One leading `<` and one trailing `>` are removed independently**, matching
  eMuleQt's `normalizeMessageId`. That makes normalisation non-idempotent, so it
  happens exactly once, where a document is read.
- **A document emitting zero lines has no identity** (`ErrNoSegments`). Otherwise
  every HTML error page in the world would share one.

`nzbmeta.CanonicalOrder` is the other half, and it is what makes an id
reproducible from a crawl: files ascending by subject then by lowest message-id,
segments ascending by number. The digest does not need it — it sorts segments
itself and is indifferent to how files are grouped — but content-addressed storage
does, because the same release assembled twice must produce the same *file*. An
imported third-party document is never reordered.

Two invariants are tested: `Identity(Parse(Encode(d))) == Identity(d)` and
`Encode(Parse(Encode(d))) == Encode(d)` byte for byte.

## Tag ids

| Tag | Id | Carries |
|---|---|---|
| `FT_META_KIND` … | `0x60`–`0x6C` | The meta row's fields |
| `FT_META_FILEINDEX` | `0x62` | The true index, authoritative above 65,535 files |
| `FT_META_TOTALSIZE` | `0x64` | The whole release's size; `FT_FILESIZE` stays the file's |
| `FT_META_ID` | `0x65` | The `catalog_id`, echoed back when asking for the metafile |
| `FT_META_MAGNET` | `0x6C` | A magnet, so a client need make no API call at all |
| `ST_META_API*` | `0x9C`, `0x9E`, `0x9F` | Where the server's metadata API is |

`0x66` is permanently unused: a server-supplied per-row URL would turn every
client into an SSRF probe. `MaxSources = 99` caps an advertised source count,
below eMule's spam heuristic.

The range `0x60`–`0x6F` is reserved for this feature and a test asserts nothing
emitted falls outside it.

## Using it from eNode-go

```go
import (
    "github.com/ModderMule/enodemeta"
    "github.com/ModderMule/enodemeta/metahash"
    "github.com/ModderMule/enodemeta/model"
    "github.com/ModderMule/enodemeta/pbconv"
)

entry := pbconv.EntryFromProto(pb)   // at the transport edge, once
if err := entry.Validate(); err != nil { /* drop the row */ }
if err := entry.Mint(); err != nil    { /* drop the row */ }
// entry.MetaHash is now the 16 bytes to advertise.
```

Before caching a metafile:

```go
if err := enodemeta.VerifyMetaFile(hash, bytes); err != nil {
    // The daemon served something that is not what it claimed. Drop it.
}
```

eNode-go must delete its own planned `proto/enode/meta/v1` and import this
instead: protobuf-go panics at init on duplicate registration of the same file
descriptor.

## Using it from eMuleQt

Generate C++ from the same `.proto` files with `protoc`, and port `metahash` and
`btid` by hand — they are small and have no dependencies. Check the port against
`testdata/meta-hash-vectors.json`, which is the point of the file existing.

The C++ side is a consumer of hashes rather than a minter: it parses, cross-
checks against the tags, and applies the four nevers.

## Using it from the Usenet crawler

The Usenet daemon reaches this module with a `replace` to the sibling checkout
rather than a copy, so there is nothing to keep in sync. It supplies the
identity and mints nothing:

```go
built, err := nzb.Build(release, nzb.Options{Generator: nzb.Generator(version)})
// built.Identity is the 32 bytes to publish, built.CatalogID the "nzb:" spelling,
// built.Bytes what db/fsmeta stores under that identity.
```

`pkg/newznab` deliberately does **not** live here. This module's dependency budget
is protobuf and connect; a newznab server needs `encoding/xml`, routing, a
category table and a quota clock, and neither other consumer wants it — eMuleQt
*is* a newznab client and eNode-go serves eD2K. The shared client-facing API this
list still defers is protobuf, not XML.

## Deferred

- ~~**`api.proto` (MetaApi)**~~ — built: `proto/enode/meta/v1/api.proto` defines
  `MetaApi` (`GetCaps`, `GetMetaFile`, `Search`) and `AccountApi` (`GetAuthStatus`,
  `Login`, `Logout`), reusing `MetaFile`, `MetaKind` and `Search*`. eNode-go serves
  it; see eNode-go's `docs/meta-api.md`.
