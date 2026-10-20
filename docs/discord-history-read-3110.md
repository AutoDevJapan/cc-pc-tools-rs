# ToDo3110: Read an explicitly authorized Discord history window

The historical audit exits before HTTP because its legacy name registry is absent. New conversation reply inspection works, but cannot read an owner-origin thread or enumerate the requested seven-day window. Keep the original registry, legacy question ledger and resident intake unchanged.

## Contract and scope

Add `unattended-runner history-read --scope <private JSON> --output <new private JSON> [--cursor <prior page JSON>]`. The scope declares domain `discord-history-read-scope-v1`, original ToDo, exact configured guild/owner, inclusive `since`, exclusive `until`, and 1–10 explicit thread IDs. Message reading never discovers additional threads. No arbitrary URL, client inventory, hidden browser state, POST/PATCH/DELETE, worker launch, acknowledgement, notification or live-state write.

Use the normal existing Discord bot credential loader in memory. Verify the bot identity, guild and configured task/request forum, thread type/owner, and a GET-readable thread starter before reading history. The caller's scope and output parent must pass the existing resource guard. Never initialize or replace an unknown registry. A rejected source remains rejected.

Here a qualified scope means the explicit IDs/window passed both validation and the existing guard; it grants no other resource access. The CLI does not directly read or reconstruct the original protection registry (resource classification) or the legacy question ledger (old question history). It invokes the existing guard, which may consult its own inputs internally. That check neither restores missing original records nor broadens the work scope.

Each call reads at most one 100-message page, newest first, and returns an explicit continuation bound to the exact scope bytes. Validate descending IDs, selected channel and timestamps. Persist only configured owner messages, bot context, IDs, edit/reference metadata and acquisition provenance in a new private receipt; never print bodies or credentials. Reject secret-bearing retained source text rather than writing authentication values. Do not reuse a cursor for a different scope or advance it after a failure. An empty page is complete only after a direct starter GET proves history read permission. Partial coverage is never all guild history.

Read-only capture is not delivery, application, verification or permission for UI changes. Current Console owner answers and the latest direct chat scope remain distinct. ToDo3084 continues independently.

## Acceptance

1. A synthetic exact scope retrieves the owner reply and bot question, preserving edit/reference metadata; stdout is metadata only.
2. Two pages and an empty terminal page resume without omissions; scope changes, repeated/non-descending/wrong-channel IDs and malformed timestamps fail without a success receipt.
3. Foreign guild/owner/forum/bot, unauthorized paths, missing starter/history permission, HTTP failure and output collisions stop without state changes or any HTTP mutation.
4. Invalid/duplicate flags and overbroad/invalid scopes stop before credential loading or HTTP. Credentials never appear in output or receipts; message bodies appear only in the selected private receipts, never stdout.
5. Existing conversation and resident tests pass. Review the fixed candidate with a different Max model, verify required CI, merge normally, deploy only the read command, and prove one explicitly authorized real page.

Primary API contract: https://docs.discord.com/developers/resources/message#get-channel-messages (newest-first, mutually exclusive before/after/around, limit 1–100); https://docs.discord.com/developers/resources/message#get-channel-message (VIEW_CHANNEL and READ_MESSAGE_HISTORY).

## Audit boundaries

The time window must start in 2015 or later, end no later than the current time and span at most 31 days. Every parent directory is checked with Lstat, including Windows junctions (irregular reparse points in Go 1.23+). A page records its last scanned ID and timestamp without third-party text. Resumption verifies that the saved continuation is derived from those fields and the prior cursor, rejecting inconsistent local receipts before loading credentials. This is consistency checking, not authentication of arbitrarily modified local receipts. Receipts are flushed before publication through a non-overwriting hard link.

The selected channel is `thread_ids[cursor.thread_index]`; a new walk starts at the first declared ID. Subsequent calls follow the validated continuation in declared order. `complete` refers only to that explicit API walk and interval, never all questions or all guild history.

## Explicit metadata inventory

The original audit requires a candidate inventory beyond the six known Console threads. `history-inventory` uses the same guarded private scope/output/cursor flags and configured credential loader. Its scope domain is `discord-history-inventory-scope-v1`, with `todo_id`, `guild_id`, `owner_id`, `since`, `until`, `forum` (`task` or `requests`, the two already configured forums), and `mode` (`active` or `archived`). It verifies the bot and forum/guild, then reads one normal active-list snapshot or one 100-thread public archive page. The archive continuation is a strictly decreasing timestamp bound to the exact scope. A 403, malformed page, missing archive continuation or changed scope produces no success receipt.

Only IDs and structural thread metadata are captured. Names and message bodies are discarded by the decoding type. Only configured bot/owner threads in the selected forum enter the candidate list. The command does not read the conversation/legacy indexes, resident state or a protection registry, and never writes those stores. Metadata completeness is not message coverage, owner reply acquisition, delivery or application. Archive pagination deliberately reaches older metadata so that earlier unfinished candidates remain visible; the caller chooses explicit message scopes next.

Inventory acceptance covers one active snapshot or one public archive page (at most 100), a scope-bound strictly decreasing archive cursor, refusal of wrong identities/corrupt cursors before further use, and absence of bodies/names/live-state changes. Deployment includes these read entrypoints and saved status in the updated existing runner, preserving merged sender features and task configuration rather than replacing them with an older standalone candidate.

The existing normal `conversation status` also returns sorted question IDs and saved reply stage IDs, without bodies, names or private paths. `intake_stages` includes configured-owner requests outside new question conversations so that absence from `reply_stages` is not mistaken for lost intake. `stage_evidence=saved_state_only` distinguishes these existing records from fresh API reads or actual application. This is the ordinary independent index/state loader, not an alternate raw-ledger reader. Status requires both saved files, creates no files, and can read during a resident operation. `snapshot_consistency=independent_saved_files` explicitly disclaims a cross-file transaction. Cache-writing replies and mutating commands retain the existing exclusive lock. Status never launches, delivers, acknowledges or closes anything.

Primary contract: [Discord thread lists](https://github.com/discord/discord-api-docs/blob/main/developers/resources/channel.mdx#list-public-archived-threads) (public archives, archive timestamp cursor, `has_more`) and [active threads](https://github.com/discord/discord-api-docs/blob/main/developers/resources/guild.mdx#list-active-guild-threads).

The first real API checks found two Console owner replies with exact question/owner identity; the original ToDo history contains their delivery markers. Standard application receipts are absent. This is missing application proof, not proof that no implementation occurred. Raw operational evidence remains outside Git.

## Review corrections and limits

Threads whose IDs exceed the exclusive time-window cursor are rejected before credential loading; they cannot produce an empty page that later refuses continuation. JSON field names that collide under the same Unicode case folding as the decoder are rejected. History records preserve an API message `type` when supplied; absent types remain unknown. An owner-authored system event is not automatically an instruction. Existing global message parsing and the sender are unchanged.

When supplied, `type` is preserved on the private history message records, not just stdout. The reply-inspection mode below keeps its fresh type/edit data in inspection metadata without changing the existing normal proof fields.

Stdout counts `owner_records` for retained owner-authored records in the `messages` array; the separate `starter` field is excluded to avoid recounting it on every page. It counts `owner_messages` only for API types 0 (default) and 19 (reply), `owner_other_types` for other supplied types, and `owner_unknown_types` when type is absent or null. The last three counts partition owner records in `messages`. Unknown and other types are never silently counted as ordinary replies. See the [official message types](https://docs.discord.com/developers/resources/message#message-object-message-types).

Only retained owner/bot bodies and the retained starter are checked for credential text. Discarded third-party and out-of-window bodies cannot block an otherwise valid audit. Their structural metadata still must pass the source/order checks. Selected credential-bearing bodies are never persisted or printed.

If a selected body blocks a page, preserve the last successful receipt and record incomplete coverage without advancing its cursor. For independent coverage, issue a new qualified scope containing only unaffected threads or a narrower time window; never modify a prior successful receipt or reuse its cursor with a changed scope. A credential-bearing starter blocks every window for that thread. Leave that thread or interval explicitly unresolved; do not retrieve secrets through another copy, print them for diagnosis, or claim the excluded coverage complete.

The caller records that unresolved interval in its own private audit status, separately from immutable API receipts. The CLI's generic failure is not a saved no-reply result and does not modify the last successful receipt.

Status does not load a Discord credential. Shared JSON reads close the handle before decoding. On Windows, atomic replacement retries only access-denied/sharing errors, at most four attempts and 70ms total delay; permanent failures retain the normal error behavior. This avoids holding a status read handle through decoding while a resident writer publishes state. It does not make the independently saved state and index transactional.

`content_sha256` checks equality, not confidentiality: short choice letters such as a/b can be guessed from an unkeyed hash; their meaning belongs to the original question. Receipts and status remain private. Public archive pagination follows the API's exclusive timestamp cursor; equal timestamps at a page boundary can leave threads unobserved. `complete` means this API walk reached its end, not proof of every historical thread or every unfinished question.

## Non-mutating inspection of registered conversation replies

`conversation replies --request-id <existing ID> --output <new .private.json>` reads an existing question and obtains its proof through the same normal `recovery-replies --read-only` association checks. It does not acquire `runner.lock`, initialize missing stores, update `latest-replies.json`, deliver, apply or close anything. The default replies command still writes its existing cache and retains the lock, as do mutating commands.

Before loading credentials, the new output's parent must be a qualified ordinary directory and the private file must not exist. Read-only inspection loads only existing state/index snapshots. Up to 100 owner records are verified, including a fresh GET of each message to retain edit timestamps, API message types and content hashes in `inspection.owner_reply_versions`. Types 0/19, other supplied types and unknown types remain distinct. The original normal proof fields are preserved alongside inspection metadata; this does not change their canonical hashes. A changed question on saved-index recheck, mismatched live reply, invalid association, source failure, selected secret-bearing text or output collision leaves no success receipt. Publication uses the existing flushed non-overwriting private-receipt writer. Stdout contains only IDs, counts, hashes and inspection metadata. The private proof is for inspection; an application still requires the current canonical per-reply source receipt and genuine application evidence. The question recheck is not a transaction across saved files or API reads and is not a guarantee against later edits.
