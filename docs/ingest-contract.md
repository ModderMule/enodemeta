# The MetaIngest contract

This is the agreement between a catalogue daemon and an eNode-go server. It is
normative: an implementation that differs from what is written here is wrong,
and the places where this repository differs from the eNode-go plan document are
listed at the end as amendments.

The service is defined in `proto/enode/meta/v1/ingest.proto` and generated into
`gen/`. Both live in this repository, released on its own, because eNode-go (Go) and eMuleQt (C++, through `protoc`)
have to agree with this daemon byte for byte.

## Who connects to whom

**The daemon serves and the server subscribes.**

The plan document originally had it the other way round, with the daemon calling
`PublishEntries` on the server. That direction forces every daemon to know an
address for every consumer, to retry into a server that may not be up yet, and
to hold a queue of what it has not managed to deliver. Turning it around removes
all three: connections run one way, a daemon needs no configuration about its
consumers at all, and several servers can subscribe to one crawler.

```
eNode-go  ──Subscribe──────────────►  torrent-crawler
          ◄─────stream of changes───
          ──FetchMetaFile──────────►
          ──Search─────────────────►
          ──GetInfo────────────────►
```

## The five calls

| Call | Shape | What it is for |
|---|---|---|
| `Subscribe` | server stream | The resumable changefeed of the published set |
| `FetchMetaFile` | unary | The `.torrent` behind one release, on demand |
| `Search` | unary | The part of the catalogue too large to publish |
| `SearchEnhanced` | unary | `Search` narrowed and described by the linked work — optional, see amendment 15 |
| `GetInfo` | unary | Who the daemon is and where its feed stands |

## The published set, and why there is one

A crawl holds millions of releases. A consumer holds the feed in memory —
eNode-go's own engine caps out around a quarter of a million rows, and one
release is several rows. So the feed is not the catalogue: it is a bounded
selection, `feed.max_published` (default 50,000) releases ranked by how well the
daemon expects them to serve a consumer, recomputed by `internal/refresher`.

**What that ranking is made of is per daemon, and not part of the contract.** A
BitTorrent daemon ranks on swarm size and popularity, because a release with no
seeders cannot be downloaded. A Usenet daemon has no swarm at all: it ranks on
article completeness, whether the shipped PAR2 data covers the shortfall, grabs,
size and age. A consumer must not reconstruct the ordering, and must not read
`seeders` as a ranking input.

A daemon nobody subscribes to can stop maintaining it: `feed.publish: false`
skips the ranking pass, so no release is ever admitted to the set and the two
ranking queries never run. That freezes the ranking rather than the set, and the
difference matters to a consumer. What is already published stays published and
is still served, but a release still leaves on `catalog.retract_after` as
`EXPIRED`, and a scrape that moves a published release's numbers is still sent
as an `UPSERT`. Since nothing refills it, a set left frozen only shrinks — and a
daemon that has *never* published answers a snapshot the way any empty daemon
does, with `reset` and nothing following it. `Search`, `FetchMetaFile` and
`GetInfo` are unaffected either way.

The size of that set is also what decides how fresh its `seeders` figures are,
and the mechanism is the daemon's own. In the BitTorrent daemon a scrape pass
covers the catalogue divided by the 60 passes in a `catalog.scrape_interval`, up
to what `catalog.max_scrape_concurrency` reaches in one tick, with the published
set scraped before the tail; a feed of 50,000 is well inside that ceiling and a
much larger one is not, so its counts age past `scrape_interval` instead. A
Usenet daemon has no tracker to ask and re-probes article availability on the
same bounded, published-set-first shape.

What the contract says is only this: a consumer must not assume any particular
freshness, and must not infer that a daemon with no `scrape_interval` setting is
not maintaining its figures.

Everything else is still reachable. `Search` covers the whole catalogue, and a
row that comes back from a search is as complete as one that came from the feed.
A consumer that treats the feed as "everything this daemon has" will be wrong
by three orders of magnitude.

## Identity: `catalog_id`

A release is named by `bt:v1:<40 hex>`, `bt:v2:<64 hex>`, `nzb:<64 hex>` or
`ed2k:<32 hex>`, uppercase hex, formatted and parsed only by `btid`. A hybrid
torrent is keyed by its **v1** hash, because that is the one both versions of the
network agree on; keying it by v2 would catalogue the same release twice. A
Usenet release is keyed by the canonical NZB digest of
[amendment 8](#amendments-to-the-enode-go-plan-document), which is recomputable
from the `.nzb` alone.

The id is unique per daemon, not globally. A consumer keys rows by
`(daemon, catalog_id)`, taking the daemon name from `GetInfo`.

## Subscribe

### The one rule

**A cursor is only ever advanced past changes that have been sent.**

Everything below follows from it, and every part of it exists because the
alternative loses a row without anyone noticing.

### Phases

A subscription has a backlog and a live tail. `after_seq` says where to resume;
`0` means "I have nothing".

```
SubscribeResponse{ changes, cursor, caught_up, reset, snapshot_end }
```

- **`reset`** — discard everything held for this daemon. What follows is a
  complete snapshot.
- **`snapshot_end`** — the snapshot is over. Rows the snapshot did not re-send
  are gone.
- **`caught_up`** — the backlog is done; everything after this is live. An
  otherwise empty message with `caught_up` set is the heartbeat.
- **`cursor`** — the highest sequence included. Persist it **after** the changes
  have been applied, never before.

### When a reset happens

1. `after_seq == 0`. The consumer has nothing.
2. `after_seq` is below the daemon's `purged_through_seq`. The consumer was away
   too long: the retractions it would need to catch up have been deleted, so an
   incremental resume would leave it advertising releases that no longer exist —
   and it would never find out. This is also checked on every pass of the live
   tail, so a purge that overtakes a connected consumer resets it mid-stream.
   A daemon running with `feed.purge_tombstones: false` never purges, so this
   case never arises there.

### The snapshot horizon

Before a snapshot starts, the daemon records `H`, the sequence the feed is
currently at. Then:

- For `feed_seq <= H`, only published rows are sent. A snapshot describes what
  the consumer should be holding.
- For `feed_seq > H`, everything is sent, published or not.

The second half is the part that is easy to get wrong. A release can be
retracted while the snapshot that already sent it is still streaming. Its
retraction gets a new sequence, above `H`, so it arrives in the live phase and
the consumer takes the release back down. A daemon that filtered the whole
stream to "currently published" would never send it, and the consumer would
advertise a dead release forever.

### Changes

```
ReleaseChange{ seq, catalog_id, op, entries, reason }
```

- `UPSERT` replaces **every** row of that `catalog_id`. It is not a merge: a
  release whose file selection changed must not leave the old rows behind.
- `RETRACT` removes them, with a reason:

| Reason | Means |
|---|---|
| `EXPIRED` | The network stopped mentioning it |
| `BLOCKED` | An operator filter now excludes it: today, the keyword filter (`filter.keywords`, [docs/filter.md](filter.md)). The release is still catalogued and still crawled; if the keywords change so it no longer matches, the next rank pass may publish it again as an ordinary `UPSERT` |
| `BELOW_THRESHOLD` | It fell out of the capped published set — still catalogued, still searchable, no longer advertised |

A release that has never been published is never sent: there is nothing to add
and nothing to retract.

### Batching and heartbeats

Batches are bounded by rows (`max_batch`, clamped by the daemon) and by bytes
(about 1 MiB), because entries carry magnets and file paths. `max_batch` from
the consumer is a hint and only ever lowers the daemon's own limit.

An idle stream sends an empty `caught_up` message every `ingest.heartbeat`
(default 30s). It doubles as the poll: a write from another process — a
`reindex`, a second crawler on the same database — does not reach this process's
change notifier, so a feed that only woke on local commits would sit still
through it.

## Minting the meta hash

**The daemon never fills `meta_hash`. The server mints it.**

Every entry carries `kind`, `identity`, `file_index`, `file_count` and
`path_authoritative`; `metahash.Mint` turns those into the 16-byte pseudo-hash.
Keeping construction in one place is what stops two implementations disagreeing
about a byte, and the vectors in `testdata/meta-hash-vectors.json`
are what keeps the C++ port honest.

The daemon still verifies: every fetched metafile is checked against the
infohash it claims to be before it is stored, and again before it is served.

## FetchMetaFile

Metafiles are pulled, not pushed. They are needed for a fraction of a percent of
rows, and pushing them all would be tens of gigabytes to serve a handful of
clicks.

```
FetchMetaFileRequest{ catalog_id, identity }
```

`identity`, when set, is checked before anything is read. A catalogue moves on —
a hybrid learns its second hash, a release is re-resolved — and a consumer that
asked for particular bytes is told they are not these rather than handed the
others.

| Condition | Code |
|---|---|
| The id is not one this daemon's kinds use | `invalid_argument` |
| No such release, its metafile is missing, or the keyword filter hides it | `not_found` |
| `identity` does not match the release | `failed_precondition` |
| The release is magnet-only (`FT_META_FLAGS` bit 3): there is no metafile | `failed_precondition` |
| The stored bytes no longer hash to their identity | `data_loss` |

For a torrent the response is a complete `.torrent`: the stored info dictionary
wrapped as `d4:info…e`, with `catalog.trackers` added as an announce-list if the
operator configured any. A DHT-only crawl has none, and a metafile with no
trackers is correct rather than broken — the infohash is enough to find the
swarm. For an NZB it is the stored document verbatim; see amendment 11.

A release the keyword filter hides answers exactly as one that was never
catalogued: `not_found`, "no release …". The answer must not tell a caller that
the release exists.

## Search

`Search` covers the catalogue beyond the published set. It is deadline-bound
(`search.timeout`, default 2s) and capped (`search.max_results`, default 100),
because a server may run it inside a user's search: it has to be the fast part
or no part.

A bare identity — 40 or 64 hex characters, with or without the `bt:vN:` or `nzb:`
prefix — is answered from the catalogue's own unique index rather than the
full-text engine, which would answer it worse and slower. 64 hex characters are
ambiguous between a v2 infohash and an NZB digest, so a daemon resolves them
against the kinds it actually serves.

The keyword filter ([docs/filter.md](filter.md)) applies twice. A `query` that
holds one of its keywords as a word answers exactly as a query that matched
nothing — no entries, `total` 0, `next_offset` 0 — without the index being
asked. And a release it hides never appears in any answer, including one for
its own infohash. `exclude` terms are not checked: excluding a keyword is the
opposite of asking for it.

A daemon with no index configured (`search.dsn` empty) answers `unimplemented`,
not an empty result, and `GetInfo` reports `search_available` false. The
distinction matters: a consumer can fall back to its own sources when told there
is no index, and cannot when told there are no matches. Infohash queries still
work without an index.

### Paging

`limit`, `offset`, `total` and `next_offset` all count **releases**, not rows. A
multi-file release comes back as its whole-set row plus a row per published
file, so a page of 20 releases can be a hundred rows.

To page, send the `next_offset` of the previous response as `offset`. Zero means
there is no further page. Do not compute offsets yourself: the daemon may have
capped `limit`, and a release it could not build rows for still counts.

Paging is bounded by a window of 1000 releases: offset plus limit never passes
it, a page that would is trimmed, and an offset at or past it returns an empty
response without searching. An infohash query has a single page; any offset
above zero is empty.

Two more fields let a client say how many pages there are without asking again:

- `total_exact` is true when `total` counts every match, and false when it is a
  lower bound — the engine stopped counting early, say at its query time limit —
  or was not counted at all. Show a false one as an estimate ("about 40 pages").
- `window` is the daemon's paging depth, 1000 unless its operator changed it.
  Zero means the daemon did not say.

The pages a client can reach are `ceil(min(total, window) / limit)`. When `total`
passes `window` that figure is certain even if `total` is not, since paging
stops at the window either way. `next_offset` stays the authority on whether
there is a next page. `model.SearchResult.Pages` does this arithmetic.

### Type

`type` is an eD2K file-type string — `Audio`, `Video`, `Image`, `Doc`, `Pro`,
`Arc`, `Iso` — matched ignoring case. It filters twice:

1. The index only selects releases that carry a row of that type, so paging and
   `total` count matching releases.
2. Within those, a file row is returned when its own type matches, and the
   whole-set row when it matches or any of the release's file rows do — it is
   how a client downloads the release the matching files belong to.

An unknown type matches nothing. The index stores types per release, so a table
upgraded from before they existed needs `torrent-crawler reindex` before
type-filtered searches see older releases.

Rows from a search are not in the feed, so a consumer must cache them itself,
keyed by the meta hash it minted. The fold is not reversible, so a later
`GetMetaFile` for a searched row can only be resolved from that cache.

## GetInfo

Returns the daemon name, version, contract version, the kinds it produces, the
feed's `last_seq` and `purged_through_seq`, the published and catalogued counts,
the number of files the catalogued releases hold (`files`), the indexer name it
puts in every row, whether search is available, whether the enhanced search
is (`enhanced_search_available`, amendment 15), and the daemon's estimates of
the size of the network it crawls (`network_users`,
`network_users_experimental` and `network_files`, amendment 21).

`files` counts files, not releases: a multi-file release yields one row per
selectable file. It is the figure an eD2K server adds to its own file total when
an operator chooses to count catalogue files in the server status.

`network_files` is a different figure: an estimate of the files the whole
network holds, most of which the daemon has never seen. It is not added to
`files`.

`purged_through_seq` is the number a consumer acts on: resume above it, or
expect a snapshot.

## Authentication

A bearer token in the `Authorization` header, checked in constant time. It may
only be empty when the listener is on loopback — a daemon on any other address
refuses to start without one, rather than coming up unauthenticated and looking
perfectly healthy.

Failures answer `unauthenticated` and nothing else. Which part was wrong is not
a caller's business.

## Transport

connect-go **v2** (`connectrpc.com/connect/v2`), over HTTP/1.1 or h2c. The
listener's `write_timeout` must stay 0: Go applies it per stream on HTTP/2, so
any value cuts off a long-lived Subscribe mid-flight. Configuration validation
rejects a non-zero value rather than letting an operator debug a feed that dies
every N seconds.

## A consumer, in order

1. `GetInfo` — learn the daemon name and the purge horizon.
2. `Subscribe` with the stored cursor, or 0.
3. On `reset`, drop everything held for this daemon.
4. Apply each change; mint each row's hash with `metahash.Mint`.
5. On `snapshot_end`, drop anything the snapshot did not re-send.
6. Persist `cursor` **after** applying, never before.
7. On `caught_up`, the feed is live; heartbeats follow.
8. `FetchMetaFile` on demand, and verify before caching.
9. `Search` for anything outside the feed, caching results under the minted hash.

`cmd/client.go` does all of it, and is the worked example.

## Amendments to the eNode-go plan document

These are the points where this repository deviates from
`eNode-go/docs/meta-search-torrent-usenet-plan.local.md`. They were agreed
before implementation.

1. **Direction.** The daemon serves and the server subscribes.
   `PublishEntries` is replaced by `Subscribe`.
2. **Two new fields on `MetaEntry`:** 17 `file_count` and 18
   `path_authoritative`. `file_count` counts the release's non-pad files and
   comes from the info dictionary, so a hash's flags stay stable across
   republishes even when the operator changes how many files are advertised.
3. **The whole-set row** uses `file_index = 0xFFFFFFFF`, which mints to `0xFFFF`
   in the hash. A single-file release gets one row with index 0 and the
   multi-file flag clear.
4. **`file_index` means libtorrent's `file_index_t`** — padding files included
   in the numbering. It is pinned by test vectors rather than asserted in prose.
5. **Enum values are prefixed** (`META_KIND_BT_V1`, `CHANGE_OP_UPSERT`). This
   changes the JSON names, which is free before the first deployment.
6. **eNode-go must import `enodemeta`** and delete its own planned
   `proto/enode/meta/v1`: protobuf-go panics on duplicate registration of the
   same file descriptor.
7. **eNode-go must require Go ≥ 1.25**, which is what this module targets.

The next five arrived with the Usenet daemon. They are worth reading together
for what they do *not* say: `META_KIND_NZB = 3`, `Kind.IdentityLen() == 32`,
`MetaFlagNeedsPAR2` and `FTMetaFilePath`'s "or an NZB's subject" were all already
in the schema, so a second kind needed **no proto change and no `gen-proto.sh`
run**. That is the payoff of a kind-agnostic contract, and it is the reason the
additions below are about meaning rather than about wire format.

8. **`catalog_id` for a Usenet release is `nzb:<64 uppercase hex>`**, the
   canonical NZB digest, not the plan's `nzb:<uuid>`. A uuid is minted, so the
   same release would be a different id after every re-crawl and after every
   restore from backup, and two indexers' copies of one posting would never
   collapse. The digest is recomputable from the `.nzb` alone, which buys
   something the torrent case does not have: `FetchMetaFile` needs no database
   round trip at all — strip `nzb:`, hex-decode, read the content-addressed
   store. The rules are in `nzbmeta/identity.go`, pinned by
   `testdata/nzb-identity-vectors.json`, and written up for a C++ port in
   usenet-crawler's `docs/nzb-identity.md`. `btid.NZB(uuid)` keeps parsing: a
   client-minted transfer id (plan §8.5) is genuinely a different object from a
   catalogue id.

9. **`seeders` and `peers` carry different units per kind.** For BitTorrent they
   are the swarm. For Usenet, `seeders` is `min(99, completion%)` — the
   percentage of claimed articles a release actually lists — and `peers` is the
   grab count, capped at `tags.MaxSources`. Both are non-zero from the first
   crawl, which is what gives ranking a signal on day one. A consumer **must not
   average or sum them across daemons**, and must not present them as a peer
   count when the row's kind is NZB.

10. **`Search.min_seeders` is accepted and dropped by a Usenet daemon.** Filtering
    on it would mean filtering on completeness, which is a different question with
    a different threshold (`catalog.min_completion`), and silently reinterpreting
    the field would be worse than ignoring it. `exclude`, `min_size`, `max_size`,
    `max_age_days` and `type` all apply normally.

11. **`FetchMetaFile` returns the stored `.nzb` verbatim** as
    `application/x-nzb`. There is no `catalog.trackers` analogue — an NZB carries
    its own `<groups>` and needs nothing added — and an operator may set
    `catalog.serve_password_meta: false` to strip `<head><meta type="password">`
    before serving. That still verifies: the identity is a digest over the article
    message-ids, so removing the head cannot move it. The
    `the same document with its head stripped` vector is exactly this case.

12. **usenet-crawler's source is NNTP, not newznab**, correcting plan §7.3. It
    crawls headers itself and builds its own NZBs; importing from a newznab
    indexer is a second `Source` implementation behind the same interface, not
    the primary one. Its ports are `:9702` for this service, `:9712` for its web
    page and `:9722` for the newznab-compatible API it serves.

13. **`SearchResponse` gained `total_exact` (4) and `window` (5)**, so that a
    client can print "page 3 of 12" and know whether 12 is a count or an
    estimate. Both are additive. A daemon that predates them leaves them zero,
    which reads as "not exact, window not stated": a client never overclaims
    from an older peer. The daemons read exactness from Manticore's
    `total_relation`, returned by the same `SHOW META` as `total_found`, so it
    costs no extra query.

14. **`SearchRequest` gained a sort (12, 13) and network-specific filters
    (14–24)**, all additive. Each one is answered from an attribute the daemon's
    Manticore index already holds, so none costs more than a comparison per
    matching release. The rule is amendment 10's, generalised: **a daemon
    ignores a filter or sort its network does not have** — a Usenet daemon drops
    `alive`, `min_leechers`, `min_popularity`, `seen_within_days` and the
    torrent sorts; a torrent daemon drops `categories`, `groups`,
    `min_completion`, `min_grabs`, `indexed_within_days` and the Usenet sorts —
    and answers an unknown or foreign sort in relevance order. `sort_ascending`
    only applies to attribute sorts. Across networks, only `DATE` and `SIZE`
    merge into one order: both networks answer them and every `MetaEntry`
    carries their keys (`age_days`, `total_size`). `SEEDERS` does not — a
    Usenet daemon answers it in relevance order, and its `seeders` is another
    unit (amendment 9) — so a multi-network search sorted by it, or by anything
    else, interleaves the networks, as before. The chosen and
    rejected filters are listed in usenet-crawler's `docs/search-filters.md`.

15. **`SearchEnhanced`, an optional search by the linked work.** A daemon that
    links releases to works — films, series, episodes, adult films and scenes —
    can narrow a search by them and describe them. It is a call of its own
    rather than more `SearchRequest` fields, because the ignore rule of amendment
    14 would make a torrent daemon answer a search for "IMDb 8 and up" with every
    match it has: a filter nobody applies is a wrong answer here, not a looser
    one. So:

    - **It is off unless the operator turns it on**, and only a daemon with a
      search index and linked works can: usenet-crawler's
      `ingest.enhanced_search`, default false. `GetInfoResponse.
      enhanced_search_available` says whether it answers. Off, or on a daemon
      that has no works at all (torrent-crawler), the call is
      **`Unimplemented`**. **A consumer checks the flag first**: eNode-go counts
      an unimplemented answer from `Search` as the daemon being down, and must
      not learn otherwise by asking.
    - **`EnhancedSearchRequest`** is a whole `SearchRequest` — every plain
      filter, sort and page applies as before — and a `WorkFilter`. Every field
      set in the filter must hold. The ids (`imdb_id`, `tmdb_movie_id`,
      `tmdb_tv_id`, `tvdb_id`, `tvmaze_id`, `tpdb_id`, `upc`) are a union among
      themselves, as newznab's id search is, and name a work's releases and
      those of every episode of a series; `season` and `episode` narrow a series
      to one season or episode; an id that names no work matches nothing.
      `performers` must all be in the work's cast, matched case- and
      punctuation-blind; a name no cast holds matches nothing. The bounds
      (`min_year` … `max_runtime`) fail a release whose value the daemon does
      not know, so "under 45 minutes" does not answer every release with no
      runtime. Scores are on a 0–100 scale; runtimes are minutes. An episode's
      year and ratings are its series'.
    - **`EnhancedSearchResponse`** is the plain `SearchResponse` and a map of
      `WorkInfo` keyed by `catalog_id`: a release is several entries, and they
      share one work. A release with no work is absent from the map. For an
      episode, `WorkInfo`'s ids, title, year and ratings are its series' — what
      a client matches a TV result by — and `episode_title`, `season` and
      `episode` its own.
    - **`MetaEntry` and the feed are unchanged**, so neither the eD2K
      pseudo-hash nor the `FT_META_*` tags move. `MetaApi` does not expose the
      call yet; eNode-go would add it the same way, behind the same flag.

16. **Music, books, anime and a release's video, in `SearchEnhanced`.** Additive,
    so a peer built against amendment 15 reads everything it did.

    - **Kinds and filters.** `WorkKind` gains `WORK_KIND_ALBUM` (6) and
      `WORK_KIND_BOOK` (7). `WorkFilter` gains:
      - `creator` (21), an album's artist or a book's author, matched the way
        `performers` are;
      - `anidb_id` (22), `anilist_id` (23), `mbid` (24, a MusicBrainz
        release-group id) and `isbn` (25, an ISBN-13), which join the union of
        ids.
    - **`season` and `episode` changed meaning** (fields 9 and 10, same
      numbers). They no longer narrow a series named by an id to its episodes.
      They now keep a release of that season, its season packs included, or one
      that carries that episode, whatever the release is linked to. A pack
      linked only to its series was lost before.
    - **`WorkInfo`** gains `creator` (17), `mbid` (18), `isbn` (19),
      `anidb_id` (20), `anilist_id` (21), and the Rotten Tomatoes and
      Metacritic page paths `rt_id` (22) and `mc_id` (23).
    - **`EnhancedSearchResponse.media`** (3) is a `MediaInfo` per `catalog_id`
      for the releases whose video header the daemon read: container, video
      codec and picture size, length in seconds, and per track the audio codecs
      and languages and the subtitle languages. A release with none is absent.
    - torrent-crawler still answers `Unimplemented`.

17. **A fourth kind, `META_KIND_ED2K` (4), and a third network,
    `META_NETWORK_KAD` (3).** They arrived with kademlia-crawler, which
    catalogues what the eMule Kad network publishes. Unlike amendments 8 to 12
    this one is a proto change, and it is the first kind that is not a
    pseudo-hash at all.

    - **An ED2K row is native.** Its `identity` is the file's 16-byte MD4, and
      that hash already belongs in the slot the pseudo-hash was invented to
      fill. So it is **not minted**: `model.Entry.Mint` copies `identity` into
      `meta_hash`, and `metahash.Build`, `Mint` and `Parse` refuse the kind
      (`ErrNativeKind`, and `ErrKind` for a hash whose kind nibble says 4).
      `Kind.Native()` and `Kind.Mintable()` are the two tests; `Kind.Valid()`
      still means "defined". The hash layout, the fold and
      `testdata/meta-hash-vectors.json` are untouched, so a C++ port has
      nothing to reproduce.
    - **The four nevers do not apply to it.** An ED2K row *is* a file hash: it
      may be searched for on Kad, used as a transfer id and written to
      `known.met`. `RejectOfferedFile` does not match it, which is correct — a
      client that has the file may offer it.
    - **One real hash in about a million carries the meta-hash marker by
      chance** (plan §3.3). Published as a native row it would read as a
      torrent or an NZB to every capable client, so `Validate` refuses it and
      the daemon skips that file.
    - **A row is one file.** `file_index` is 0, `file_count` is 1 (or unset),
      `total_size` equals `size` (or is unset), and there is no whole-set row.
      `path_authoritative`, `magnet` and the torrent and Usenet flags
      (`MagnetOnly`, `PrivateTracker`, `V2Available`, `NeedsPAR2`) are refused.
    - **`catalog_id` is `ed2k:<32 uppercase hex>`**, `btid.ED2K(hash)`: the
      namespace `btid` already parsed. Like amendment 8's it is recomputable,
      so two daemons' rows for one file collapse.
    - **There is no metafile.** `FetchMetaFile` answers `NotFound` for the
      kind and `enodemeta.IdentityOf` returns `ErrNoMetaFile`. The complete
      instruction for a client is the eD2K link, which `ed2klink.Build` makes
      from the row's own `name`, `size` and `identity`; it is not carried on
      the row.
    - **`seeders` and `peers` are a third unit** (amendment 9). `peers` is the
      sources the network reported for the file and `seeders` the complete
      ones. They are what a Kad search result shows, and the same rule holds:
      never summed or averaged across daemons. **The daemon caps them, and the
      cap is the operator's**: kademlia-crawler's `catalog.max_sources`
      defaults to `tags.MaxSources` (99) and 0 advertises the true count. The
      cap keeps a row out of eMule's source-count spam heuristic, which needs
      more than 100 sources on a result that one UDP server alone returned; a
      popular Kad file really has more, so capping trades a true count for
      staying out of it. kademlia-crawler's `docs/emule-spam.md` has the
      conditions.
    - **Filters follow amendment 14's ignore rule.** A Kad daemon answers
      `min_seeders` against sources (amendment 20; this said complete sources
      before) and drops the torrent- and Usenet-only ones.
    - **A server that predates this amendment is not harmed by it.** proto3's
      open enums carry the new values through as numbers: a server that does
      not know kind 4 fails `KindFromProto` to unspecified and drops the row,
      which is the safe side. Amendment 18 is what eNode-go and eMuleQt do
      with the row.

18. **A native row is marked on the wire by one tag, `FT_META_NETWORK`
    (0x6D).** It is a uint8 holding a `MetaNetwork` value, and
    `META_NETWORK_KAD` (3, `tags.MetaNetworkKad`) is the only one in use. This
    is a tag number, not a proto change.

    - **It is the row's only meta tag.** The server sends an ED2K row as an
      ordinary search result — the real MD4, no source address, the classic
      name, size, type and source tags — plus this one. `FT_META_KIND` is
      **not** sent: a client that knows the meta rows drops a row whose kind
      tag its hash cannot back, so a kind tag on a real MD4 would make the
      file vanish for every client already in the field.
    - **The tag, not the name, is how a client knows.** The server may prefix
      the name (`[kad] `, the operator's choice, and it may be empty) for the
      clients that do not read the tag. A client that does read it shows the
      network its own way and drops a leading `[kad …]` bracket, and only
      then: a file that is merely named that way keeps its name.
    - **A client that does not know the tag has an ordinary file.** Both eMule
      trees keep an unknown numeric tag of a known type and ignore it, and a
      uint8 is a type both parse.
    - **The server's own file wins.** When the hash is one a user shares on
      the server, the server sends that row alone, with its real sources and
      without the tag or the prefix.
    - **Every client gets these rows.** The capability bits of plan §5 gate
      pseudo-hash rows, which a stock client cannot act on. A native row it
      can simply download.

19. **`MetaApi.Search` serves the Kad network as a third one.** No proto
    change: `META_NETWORK_KAD` and `META_KIND_ED2K` are amendment 17's. This
    one says what the client-facing search does with them.

    - **`META_NETWORK_UNSPECIFIED` is every network the server offers**, Kad
      included, and `Caps.networks` lists it. A client that wants torrent and
      Usenet without Kad asks with `kinds`.
    - **Releases alternate in the order torrent, Usenet, Kad**, one from each,
      and a network that runs out drops out. DATE and SIZE merge by key, as
      amendment 14 says; an ED2K row that leaves `total_size` unset is
      compared by `size`.
    - **An ED2K entry has no metafile and no magnet.** Its `meta_hash` is the
      file's MD4, so `GetMetaFile` refuses it as `invalid_argument`: it is not
      a meta hash. `Caps.kinds` is the kinds whose metafiles are served and
      never lists ED2K. The client builds the eD2K link with `ed2klink.Build`.
    - **The name is the file's own.** The `[kad] ` prefix of amendment 18
      belongs to the eD2K search result and is not put on an API row.
    - **The server's own file wins here too, by rewriting the entry.** When a
      connected user shares a file with the same hash and size, the entry
      carries the server's `name`, and its own source counts as `peers` and
      `seeders`. It is not dropped as amendment 18 drops the eD2K row: this
      search has no row of the server's to send instead. The entry keeps its
      place, so paging and `total` are the daemon's.

20. **A Kad daemon answers `Search.min_seeders` against sources**, correcting
    amendment 17, which said complete sources. No proto change.

    - **Why.** Kad carries no count of complete sources. Stock eMule's
      keyword publish sends one count, `TAG_SOURCES`, and no
      `TAG_COMPLETE_SOURCES`. Of 34,690 files kademlia-crawler catalogued
      from search results, 28,513 came with a source count and 10 with a
      count of complete sources, so the old mapping filtered out nearly
      every file for any `min_seeders` of 1 or more.
    - **What it compares.** The source count the daemon holds, before its
      cap (`catalog.max_sources`, 99 by default). A file with 500 sources
      matches `min_seeders` 200 and its row still says `peers` 99.
    - **What does not change.** A row's `seeders` is still the complete
      sources and `peers` the sources. `SEARCH_SORT_SEEDERS` still orders by
      complete sources and `SEARCH_SORT_LEECHERS` by sources. `min_leechers`
      is still dropped by a Kad daemon.
    - **For a consumer:** a returned ED2K row may have `seeders` below
      `min_seeders`, and `peers` below it too when the cap applies. A server
      must not filter the daemon's rows by either again.

21. **`GetInfo` carries the daemon's estimate of the network's size.** Three
    fields, `network_users` (13), `network_users_experimental` (14) and
    `network_files` (15).

    - **Two user counts, because eMule makes two.** `network_users` comes
      from how densely the daemon's routing table is filled near its own id
      and is the number eMule shows. `network_users_experimental` comes from
      how close the nearest node of each lookup came to its target, averaged
      over many lookups, and is the one eMule labels experimental.
    - **`network_files` is the whole network's, not the catalogue's.** A
      daemon estimates it from the user count; how is its own business and
      its documentation says. `files` stays the count of what is catalogued.
    - **Zero is no estimate**, not an empty network: a daemon that has just
      started, has too few lookups for the experimental figure, or does not
      estimate at all sends zero. A consumer must not read it as a count.
    - **The figures are the network's, not this daemon's or its consumer's.**
      A server that adds `network_users` to the user count it advertises is
      counting users who are not connected to it, which is an operator's
      choice to make and off unless made.
    - **A peer that predates this amendment is not harmed by it.** An older
      daemon omits the fields and proto3 reads them as zero; an older server
      ignores them.
