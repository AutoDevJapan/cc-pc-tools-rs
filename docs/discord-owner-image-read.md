# Read one image attached to a verified owner reply

Attachment observations in `history-read` deliberately retain only IDs and sizes.
That command remains metadata-only. A separate `history-attachment --scope <private
JSON> --output <new .private.image|.private.png|.private.jpg|.private.webp|.private.gif>` reads the
single image already identified on an owner reply, using the existing bot loader.
It never launches a worker, changes intake, sends a message, or opens a browser.

The private scope embeds the existing history scope fields with domain
`discord-owner-image-read-scope-v1` and exactly one thread. It additionally declares
`message_id`, `attachment_id`, `expected_content_sha256`, `expected_created_at`,
`expected_edited_timestamp` (empty means unedited), and `expected_size` in bytes.
All IDs, the existing finite time window, qualified input/output parent, and absent
private output are checked before loading credentials. The body SHA and timestamps
bind the image to the exact owner reply version already read by the caller.

Verify the configured bot, guild, configured task/request forum, thread owner and
readable starter, then GET the exact selected message. It must be an ordinary owner
message (type 0/19) with the expected body/version and one matching attachment ID and
size. Download only the fresh API-returned HTTPS `cdn.discordapp.com/attachments/`
URL whose channel and attachment path segments match. Do not accept arbitrary URLs,
ports, userinfo, fragments or redirects. The CDN request has no Discord Authorization
header or cookies. URL signatures, filenames, descriptions and response error bodies
are never printed or saved. No extra authentication or API key is introduced.

The selected file must be a PNG/JPEG/WebP/GIF, at most 20 MiB, with a matching detected
media type/output extension. The retrieved body must match its HTTP response length
when supplied. Keep the original API metadata size bound to the selected attachment,
and record that `source_size` separately from the downloaded `size`. The image SHA
describes the exact retrieved CDN representation, not a verified hash of the original
upload. Do not infer why a CDN representation's size differs. With `.private.image`,
detect the supported image type without requiring the caller to guess a format
that the metadata-only history receipt deliberately omits. Recheck the exact API reply
version and attachment identity after download. Publish the unmodified bytes with
the existing flushed, non-overwriting private writer only after all checks succeed.
Stdout returns IDs, version timestamps/body SHA, media type, size, scope SHA and image
SHA; it contains neither message text, CDN URL nor credentials. A failure publishes
no image and reports only a fixed failure-stage label. Existing images/receipts are
not replaced. A successful read does not imply
delivery to the handler, actual application, approval for another operation or close.

Acceptance covers a mention-only owner reply with an image, exact bytes/provenance,
empty CDN authentication, invalid scope/output rejection before loading, wrong owner/
guild/forum/attachment/version, URL and redirect rejection, bounded invalid media or
size, and changed reply during download without publishing an output. Real account
or message values remain in private operational evidence, never public fixtures.

Primary contracts: [Discord attachments](https://docs.discord.com/developers/resources/message#attachment-object)
and [fresh signed attachment URLs](https://docs.discord.com/developers/reference#signed-attachment-cdn-urls).
