# ToDo3110: Read an explicitly authorized Discord history window

The historical audit exits before HTTP because its legacy name registry is absent. New conversation reply inspection works, but cannot read an owner-origin thread or enumerate the requested seven-day window. Keep the original registry, legacy question ledger and resident intake unchanged.

## Contract and scope

Add `unattended-runner history-read --scope <private JSON> --output <new private JSON> [--cursor <prior page JSON>]`. The scope declares domain `discord-history-read-scope-v1`, original ToDo, exact configured guild/owner, inclusive `since`, exclusive `until`, and 1–10 explicit thread IDs. Message reading never discovers additional threads. No arbitrary URL, client inventory, hidden browser state, POST/PATCH/DELETE, worker launch, acknowledgement, notification or live-state write.

Use the normal existing Discord bot credential loader in memory. Verify the bot identity, guild and configured task/request forum, thread type/owner, and a GET-readable thread starter before reading history. The caller's scope and output parent must pass the existing resource guard. Never initialize or replace an unknown registry. A rejected source remains rejected.

Each call reads at most one 100-message page, newest first, and returns an explicit continuation bound to the exact scope bytes. Validate descending IDs, selected channel and timestamps. Persist only configured owner messages, bot context, IDs, edit/reference metadata and acquisition provenance in a new private receipt; never print bodies or credentials. Reject secret-bearing source text rather than writing authentication values. Do not reuse a cursor for a different scope or advance it after a failure. An empty page is complete only after a direct starter GET proves history read permission. Partial coverage is never all guild history.

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

## Explicit metadata inventory

The original audit requires a candidate inventory beyond the six known Console threads. `history-inventory` uses the same guarded private scope/output/cursor flags and configured credential loader. Its scope domain is `discord-history-inventory-scope-v1`, with `todo_id`, `guild_id`, `owner_id`, `since`, `until`, `forum` (`task` or `requests`, the two already configured forums), and `mode` (`active` or `archived`). It verifies the bot and forum/guild, then reads one normal active-list snapshot or one 100-thread public archive page. The archive continuation is a strictly decreasing timestamp bound to the exact scope. A 403, malformed page, missing archive continuation or changed scope produces no success receipt.

Only IDs and structural thread metadata are captured. Names and message bodies are discarded by the decoding type. Only configured bot/owner threads in the selected forum enter the candidate list. The command does not read the conversation/legacy indexes, resident state or a protection registry, and never writes those stores. Metadata completeness is not message coverage, owner reply acquisition, delivery or application. Archive pagination deliberately reaches older metadata so that earlier unfinished candidates remain visible; the caller chooses explicit message scopes next.

Primary contract: [Discord thread lists](https://github.com/discord/discord-api-docs/blob/main/developers/resources/channel.mdx#list-public-archived-threads) (public archives, archive timestamp cursor, `has_more`) and [active threads](https://github.com/discord/discord-api-docs/blob/main/developers/resources/guild.mdx#list-active-guild-threads).

The first real API checks found two Console owner replies with exact question/owner identity; the original ToDo history contains their delivery markers. Standard application receipts are absent. This is missing application proof, not proof that no implementation occurred. Raw operational evidence remains outside Git.
