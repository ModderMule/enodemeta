# The ServerSearch contract

This is the agreement between two eD2K servers that search and browse each
other's file catalogue. It is normative: an implementation that differs from
what is written here is wrong.

The service is defined in `proto/enode/meta/v1/server.proto` and generated into
`gen/`. The domain types are in `model/server.go` and the conversions in
`pbconv/server.go`. eNode-go is the first implementation; its side is described
in eNode-go's `docs/server-search.md`.

## Who connects to whom

**Every server serves, and every server may call.** The service is symmetric:
two servers that both implement it each hold a client for the other.

```
server A  ──GetServerInfo──────────►  server B
          ──SearchFiles────────────►
          ──BrowseFiles────────────►
          ◄──────────the same three──
```

`MetaIngest` runs server → daemon and `MetaApi` runs client → server. Neither is
reused here, because what a server tells another server is narrower than what it
tells either of those.

## The three calls

| Call | Shape | What it is for |
|---|---|---|
| `GetServerInfo` | unary | What the server offers and the limits it applies |
| `SearchFiles` | unary | A keyword search, paged by offset |
| `BrowseFiles` | unary | A walk of the whole catalogue, paged by token |

## What the catalogue is

**The files the answering server's own connected users share, and nothing
else.** Two things are deliberately left out:

- **Rows from a catalogue daemon.** A torrent, Usenet or Kad row came from a
  `MetaIngest` daemon. A server that wants those runs its own daemons.
- **Rows learned from another server.** A server that mirrors its neighbours
  must never serve the mirror. Otherwise a file travels round a ring of servers
  and outlives the last user who shared it.

So every `ServerFile` is a real eD2K file: `hash` is its 16-byte MD4, and `hash`
with `size` is its identity.

## No client is ever identified

A user chose the server they logged into. They did not choose the servers it
talks to. So:

- No message carries an IP address, a port, a client id or a user hash.
- No call returns a source. There is no "get sources" in this service, and one
  must not be added.
- A row says that a file exists and how many users of the answering server
  share it. Finding a source stays between the client and the networks it
  already uses: global source queries, Kad, source exchange.

`sources` and `complete_sources` are counts. A count of one does say that
exactly one user of that server shares the file; it does not say which. A server
that considers even that too much leaves the file out of its answers, which the
contract allows: a server need not serve every file it holds.

A field that could identify a client must never be added to `server.proto`.
`pbconv` has a test, `TestServerFileIdentifiesNoClient`, that lists the fields
of `ServerFile` and fails when one is added, so the question is asked at the
moment it matters.

**Counts from several servers are not added up.** A caller that holds the same
file from two servers shows the larger count. Nothing says the users differ.

## GetServerInfo

| Field | Meaning |
|---|---|
| `contract_version` | Highest major version served. This document is version 1 |
| `name` | The server's display name |
| `files` | How many files the catalogue holds |
| `search_available`, `browse_available` | Whether each call answers. False, the call reports `unimplemented` |
| `max_search_limit`, `max_browse_limit` | The most files one answer carries. A larger `limit` is capped, not refused |
| `browse_min_interval_seconds` | How long to wait between two `BrowseFiles` calls. Zero asks for no wait |
| `catalog_epoch` | Changes when the catalogue was rebuilt rather than edited |

A caller asks it before anything else, and again before each catalogue walk.

## SearchFiles

**Request.** `query` is keywords separated by spaces, and every one must match
the file name. `exclude` lists keywords none of which may match. `type` is an
eD2K file type (`Audio`, `Video`, `Image`, `Doc`, `Pro`, `Arc`, `Iso`).
`min_size`, `max_size` and `min_sources` are bounds, zero meaning none.

**Paging is by offset.** The caller asks for the next page with `next_offset`
rather than computing it, since the server caps `limit`. Zero means there is no
further page. Pages are stable while the server caches the query, a few minutes;
after that the same offset may answer differently.

**A search reaches a bounded number of files**, and `total` counts what it
reached, not what exists. It is not a way to read the whole catalogue.

**Order** is the server's own. A caller that merges answers from several servers
orders them itself.

## BrowseFiles

**Paging is by token.** The first call sends no `page_token`. Each later call
sends the `next_page_token` of the answer before it, unchanged. An empty
`next_page_token` ends the walk. The token is opaque: a caller neither reads nor
builds one, and a token is only good for the server that issued it.

**A walk is not a snapshot.** The catalogue changes under it: users log in and
out all the time. While a walk runs,

- a file that appears may be missed,
- a file that disappears may still be reported,
- a file may be reported twice.

The order means nothing and may differ from one walk to the next.

### Reset

When the server can no longer continue from a token, it answers with `reset`
set, no files and no token. That happens when the catalogue was rebuilt: a
restart, or an index compaction that renumbered what the token pointed at. A
token of an earlier `catalog_epoch` always answers `reset`.

The caller starts again without a token. **It must not treat the abandoned walk
as complete**: the pages it did read are a partial view.

`reset` is an answer, not an error. `invalid_argument` is for a token the server
did not issue at all.

### Mirroring a catalogue

A caller that keeps a copy does it by walking, and the rules above decide how:

1. Call `GetServerInfo`. Stop if `browse_available` is false.
2. Walk from an empty token, waiting `browse_min_interval_seconds` between
   calls, and mark every file read with this walk's number.
3. On `reset`, go back to step 2 with a new walk number and delete nothing.
4. When a walk completes, delete the files of that server that this walk did not
   mark. That is the only way a file leaves the mirror: there is no retraction
   message, and a file is kept until a whole later walk completes without it.
5. Drop everything from a server that has not completed a walk for a long time.
   A mirror of a server that went away is a list of files nobody can vouch for.

A mirror is for answering the caller's own users. It is never served to another
server (see "What the catalogue is").

## Errors

Every error carries an `ErrorInfo` detail (`api.proto`) with a `msg_code`.

| Code | When | `msg_code` |
|---|---|---|
| `unauthenticated` | No credential, or one the server does not know | `serversearch.unauthorized` |
| `permission_denied` | The caller is known and not allowed | `serversearch.unauthorized` |
| `resource_exhausted` | Rate limit | `ratelimit.exceeded` |
| `invalid_argument` | `SearchFiles` without a keyword | `search.query_required` |
| `invalid_argument` | A query that is too long | `search.query_too_long` |
| `invalid_argument` | A page token the server did not issue | `serversearch.cursor_invalid` |
| `unimplemented` | The service is off | `serversearch.disabled` |
| `unimplemented` | `BrowseFiles` is off | `serversearch.browse_disabled` |
| `unimplemented` | `SearchFiles` is off | `search.disabled` |
| `unavailable` | The server could not read its catalogue | `search.unavailable` |

Which part of a credential was wrong is not a caller's business: an
authentication failure says nothing more than that.

## Authentication

A server decides who may call it, in one of two ways. Both may be combined.

**A shared token.** Two operators agree on a token and each configures the
other's URL with it. The caller sends it as a bearer token in the
`Authorization` header and the server compares it in constant time against the
tokens of the servers it knows. One token serves both directions of a pair.

**Being a known server.** A server may accept any caller whose address its own
server list has verified as an eD2K server, with no token. This is open to
anyone who runs a server, so it is rate limited by address and is a choice the
operator makes, not the default.

A server that requires a token must not accept one over an unencrypted
connection from anywhere but loopback.

## Discovery

A server learns where another one serves `ServerSearch` in one of two ways:

- **Configuration.** The operator writes the URL down. Nothing is advertised.
- **The UDP description reply.** A server that accepts known servers says so in
  `OP_SERVER_DESC_RES`, which one server already asks of another:

| Tag | Id | Type | Carries |
|---|---|---|---|
| `ST_SERVER_SEARCH` | `0xA0` | string | The service's base URL |
| `ST_SERVER_SEARCH_FP` | `0xA1` | string | `sha256/<base64>` of the certificate's SubjectPublicKeyInfo |

The tag is the whole advertisement. A reader that does not know a tag skips
it; that is what both eMule client trees and the original eserver do.

**No flag bit is sent.** `FlagServerSearch` (`0x20000`, `tags/tags.go`) is
reserved for the server flags word and must not be set. Lugdunum eserver 17.14
keeps a peer that sets it but then answers every peer-list request with an empty
list, which removes that peer from the server mesh.

**A caller dials the address it already trusts.** The URL in the tag is the
peer's own claim. A caller takes the port, the host header and the TLS name from
it, and connects to the address its server list verified the peer at. Following
the URL's host instead would let any server point another at a third party.

The fingerprint is how a server on a bare IP address is trusted without a
certificate authority: the caller pins it, in the form `ST_META_API_FP` already
uses for the Meta API. A caller that was given a fingerprint, by tag or by
configuration, must refuse a certificate that does not match it.

## Transport

connect-go **v2** (`connectrpc.com/connect/v2`), over HTTP/2 with TLS, or h2c
without. All three calls are unary, so the usual server timeouts apply.

An answer is bounded by `max_browse_limit` files, which a server sets so that a
page stays well under a megabyte.

## A caller, in order

1. Learn the URL: configuration, or the description reply.
2. `GetServerInfo`. Check `contract_version` and what is available.
3. To answer a user's search: `SearchFiles`, with a short deadline. A server
   that does not answer in time is skipped, not waited for.
4. To mirror: `BrowseFiles`, as in "Mirroring a catalogue".
5. Back off from a server that answers `unavailable`, `unimplemented` or
   `unauthenticated` instead of calling it again on the next search.
