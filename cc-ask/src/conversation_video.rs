//! Bounded MP4 container validation for ordinary, source-qualified attachments.
//! Generated-image authority remains separate; this does not decode codecs.
use crate::core::Result;

pub(crate) const LIMIT: usize = 20 * 1024 * 1024;

pub(crate) fn validate(bytes: &[u8]) -> Result<()> {
    fn walk(
        mut bytes: &[u8],
        parent: &[u8],
        depth: usize,
        state: &mut (bool, bool, bool),
    ) -> Option<()> {
        if depth > 4 {
            return None;
        }
        while !bytes.is_empty() {
            let first = bytes.get(..8)?;
            let kind = &first[4..8];
            let mut size = u32::from_be_bytes(first[..4].try_into().ok()?) as u64;
            let mut header = 8;
            if size == 1 {
                size = u64::from_be_bytes(bytes.get(8..16)?.try_into().ok()?);
                header = 16;
            }
            if size == 0 {
                size = bytes.len() as u64;
            }
            let size = usize::try_from(size).ok()?;
            let payload = bytes.get(header..size)?;
            if depth == 0 && kind == b"ftyp" {
                if state.0 || payload.len() < 8 || (payload.len() - 8) % 4 != 0 {
                    return None;
                }
                let allowed = |brand: &[u8]| {
                    matches!(
                        brand,
                        b"isom" | b"iso2" | b"mp41" | b"mp42" | b"avc1" | b"dash" | b"M4V "
                    )
                };
                state.0 = allowed(&payload[..4]) || payload[8..].chunks_exact(4).any(allowed);
                if !state.0 {
                    return None;
                }
            } else if depth == 0 && kind == b"mdat" {
                state.2 |= !payload.is_empty();
            } else if (depth == 0 && kind == b"moov")
                || (parent == b"moov" && kind == b"trak")
                || (parent == b"trak" && kind == b"mdia")
            {
                walk(payload, kind, depth + 1, state)?;
            } else if parent == b"mdia" && kind == b"hdlr" {
                state.1 |= payload.get(8..12) == Some(b"vide");
            }
            bytes = bytes.get(size..)?;
        }
        Some(())
    }
    let mut state = (false, false, false);
    if bytes.len() > LIMIT
        || bytes.get(4..8) != Some(b"ftyp")
        || walk(bytes, b"", 0, &mut state).is_none()
        || state != (true, true, true)
    {
        return Err("invalid or oversized MP4 video attachment".into());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    fn box_bytes(kind: &[u8; 4], payload: &[u8]) -> Vec<u8> {
        let mut out = ((payload.len() + 8) as u32).to_be_bytes().to_vec();
        out.extend_from_slice(kind);
        out.extend_from_slice(payload);
        out
    }
    fn fixture(handler: &[u8; 4], size: usize) -> Vec<u8> {
        let mut h = [0; 12];
        h[8..].copy_from_slice(handler);
        let mut out = box_bytes(b"ftyp", b"isom\0\0\0\0isommp42");
        out.extend(box_bytes(
            b"moov",
            &box_bytes(b"trak", &box_bytes(b"mdia", &box_bytes(b"hdlr", &h))),
        ));
        out.extend(box_bytes(b"mdat", &vec![0; size]));
        out
    }
    #[test]
    fn bounded_mp4_requires_video_track_media_and_complete_boxes() {
        let valid = fixture(b"vide", 1);
        assert!(validate(&valid).is_ok());
        for bad in [
            fixture(b"soun", 1),
            fixture(b"vide", 0),
            valid[..valid.len() - 1].to_vec(),
            b"ftyp isom".to_vec(),
        ] {
            assert!(validate(&bad).is_err());
        }
        let at_limit = fixture(b"vide", LIMIT - (valid.len() - 1));
        assert_eq!(at_limit.len(), LIMIT);
        assert!(validate(&at_limit).is_ok());
        let mut over = at_limit;
        over.push(0);
        assert!(validate(&over).is_err());
        let mut huge = valid;
        huge[..4].copy_from_slice(&1u32.to_be_bytes());
        huge[8..16].copy_from_slice(&u64::MAX.to_be_bytes());
        assert!(validate(&huge).is_err());
    }
}
