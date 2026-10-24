# Deliver attachment observations with an owner answer

The normal conversation reply projection contains text but drops attachment
observations. A mention-only answer can therefore reach its handler without the
image needed to interpret it. Keep the existing canonical answer/source hashes,
wait generation, application validation and sender behavior intact.

After ordinary reply delivery, GET that exact current owner message and validate
its owner, channel, body, creation/edit times and ordinary message type. Retain
only attachment IDs and sizes, never names, descriptions or signed URLs. Unknown
attachment metadata remains unknown. A known empty list is not an image answer.

For a nonempty attachment list, save a separate immutable private observation
bound to the original question, current reply version and canonical source SHA.
Give the handler a normal note supplement with that private reference and one
finite `history-attachment` scope per eligible attachment. A known removal after
an earlier observation must also reach the handler. Attachments outside the image
reader's size/format contract remain observations, not successful image reads.

Publish the versioned observation and scopes before editing the original ToDo.
Use the normal exact-note SHA compare and read the marker back. Do not change
the task's status, worker, dependencies or owner stops. Retain old observations;
retries must reuse matching bytes and avoid duplicate supplements. Update only
the separate latest-observation pointer after successful readback. Recheck the
live owner version and attachments before the note edit. API or note failure
does not advance that pointer or claim delivery. A matching latest observation
means its supplement was delivered; never replay it after the note is replaced.
New supplements require the current reply's original delivery marker (or the
same media marker) in the note. A replaced or unrelated hold is left intact.
Media failures have their own health label and retry on a later normal tick;
they do not prevent the already-delivered text's original application/close
checks. Existing applied/closed records are never manufactured by this supplement.

The supplement follows ordinary reply delivery. For a question that releases a
worker wait, the worker may start before this supplemental note arrives. No claim
is made that attachment metadata is present at worker launch. The existing
private answer reference remains available; the handler must finish reading all
answer material before recording application.

Acceptance:

- A mention-only owner image yields an exact attachment reference and a usable
  fixed scope in the original handler note; canonical source bytes stay intact.
- Identical retries do not append twice; an attachment edit/removal creates a
  new observation and preserves the old one.
- Wrong owner/channel/version, malformed/duplicate IDs, invalid sizes and a
  second-read change produce no note edit or latest-observation success.
- Unknown/empty metadata without a prior image does not invent an image.
- A failed note comparison can retry through the same ordinary path without
  overwriting the original note, releasing waits or fabricating application.

This change delivers observation metadata, not image meaning or product completion.
The handler must read the image and verify the effect in the original task scope.
Progress-thread origin association is a separate unfinished repair.
