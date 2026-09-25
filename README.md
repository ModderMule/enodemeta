# enodemeta

The shared contract between the eNode-go server, its catalogue daemons
(torrent-crawler, usenet-crawler) and the eMuleQt client: the `enode.meta.v1`
protobuf service `MetaIngest`, its generated connect-go code, and the code that
has to produce identical bytes in every repository — the eD2K pseudo-hash, the
`bt:`/`nzb:` ids, the `FT_META_*` tag ids, and torrent/NZB identity parsing.

Module path: `github.com/ModderMule/enodemeta` (Go ≥ 1.25). Its only dependencies
are `google.golang.org/protobuf` and `connectrpc.com/connect/v2`.

## Consumers

| Repository | How it imports this module |
|---|---|
| torrent-crawler | `replace github.com/ModderMule/enodemeta => ../enodemeta` (clone side by side) |
| usenet-crawler | `replace github.com/ModderMule/enodemeta => ../enodemeta` (clone side by side) |
| eNode-go | git submodule at `./enodemeta`, `replace ... => ./enodemeta` |
| eMuleQt | generates C++ from `proto/`, ports `metahash` and `btid` by hand against `testdata/` |

A consumer must import the generated code from here rather than generating its
own copy of `proto/enode/meta/v1`: protobuf-go panics at init when the same file
descriptor is registered twice.

## Layout

| Path | What it holds |
|---|---|
| `proto/`, `gen/` | The `.proto` files and the generated protobuf + connect code (checked in) |
| `metahash/` | The eD2K pseudo-hash: minting, parsing, cross-checking |
| `btid/`, `magnet/` | The `bt:v1:`/`bt:v2:`/`nzb:` id namespace; magnet links |
| `model/`, `pbconv/` | Domain types without protobuf, and the conversions at the edge |
| `tags/`, `filetype/` | `FT_META_*` tag ids and flags; extension → eD2K file type |
| `bencode/`, `torrentmeta/`, `nzbmeta/` | Metafile parsing and identity |
| `verify.go` | `VerifyMetaFile`: recompute a metafile's identity and check it against a hash |
| `testdata/` | Cross-language vectors |

## Development

```sh
go test ./...
scripts/gen-proto.sh   # only after editing a .proto; needs protoc
```

## Documentation

- [docs/ingest-contract.md](docs/ingest-contract.md) — the normative `MetaIngest` contract
- [docs/enodemeta.md](docs/enodemeta.md) — the library, the pseudo-hash, and how each consumer uses it
