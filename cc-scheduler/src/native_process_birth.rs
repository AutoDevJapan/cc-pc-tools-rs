//! Read-only process birth metadata without opening another process's handle.
//! The SystemProcessInformation ABI is runtime-checked against our own handle.
//! No command lines, environments, memory, tokens or image-name pointers are read.
use std::{collections::BTreeMap, ffi::c_void, mem::size_of};

type Births = BTreeMap<u32, (u32, u64)>;
type Query = unsafe extern "system" fn(u32, *mut c_void, u32, *mut u32) -> i32;
const MAX_BYTES: usize = 16 * 1024 * 1024;
const LENGTH_MISMATCH: i32 = 0xc0000004u32 as i32;

#[repr(C)]
#[derive(Clone, Copy)]
struct UnicodeString {
    length: u16,
    maximum: u16,
    buffer: *const u16,
}

// Prefix of SystemProcessInformation, class 5. See Microsoft's NtQuerySystemInformation
// documentation and the Windows SystemProcessInformation ABI. Only scalar identity
// fields are consumed. The reserved timing layout is never trusted without an exact
// same-query comparison with GetProcessTimes for our own process.
#[repr(C)]
#[derive(Clone, Copy)]
struct Header {
    next: u32,
    threads: u32,
    reserved: [u64; 3],
    created: i64,
    user: i64,
    kernel: i64,
    image: UnicodeString,
    priority: i32,
    pid: usize,
    parent: usize,
}

fn decode(bytes: &[u8]) -> Option<Births> {
    if bytes.len() > MAX_BYTES {
        return None;
    }
    let mut offset = 0usize;
    let mut result = Births::new();
    loop {
        let end = offset.checked_add(size_of::<Header>())?;
        if end > bytes.len() {
            return None;
        }
        // SAFETY: the complete fixed prefix is within this byte slice. Unaligned
        // reading is intentional; every field permits arbitrary bit patterns and
        // the embedded image pointer is never dereferenced.
        let row = unsafe { bytes.as_ptr().add(offset).cast::<Header>().read_unaligned() };
        let pid = u32::try_from(row.pid).ok()?;
        let parent = u32::try_from(row.parent).ok()?;
        if pid != 0 && row.created > 0 && result.insert(pid, (parent, row.created as u64)).is_some()
        {
            return None;
        }
        if row.next == 0 {
            return Some(result);
        }
        if (row.next as usize) < size_of::<Header>()
            || !(row.next as usize).is_multiple_of(size_of::<usize>())
        {
            return None;
        }
        offset = offset.checked_add(row.next as usize)?;
    }
}

pub(super) fn snapshot() -> Option<Births> {
    use windows_sys::Win32::System::{
        LibraryLoader::{GetModuleHandleW, GetProcAddress},
        Threading::GetCurrentProcessId,
    };
    let library: Vec<u16> = "ntdll.dll\0".encode_utf16().collect();
    // SAFETY: the name is NUL-terminated and GetModuleHandle does not transfer an
    // owned reference. Ntdll is already loaded for this process's lifetime.
    let module = unsafe { GetModuleHandleW(library.as_ptr()) };
    if module.is_null() {
        return None;
    }
    // SAFETY: this fixed export has the Query signature; the information class
    // and output layout are closed inside this module, never caller-selected.
    let query: Query = unsafe {
        std::mem::transmute(GetProcAddress(
            module,
            c"NtQuerySystemInformation".as_ptr().cast(),
        )?)
    };
    let mut length = 64 * 1024usize;
    for _ in 0..8 {
        if length > MAX_BYTES {
            return None;
        }
        // u64 allocation gives the native API eight-byte aligned writable storage.
        let mut storage = vec![0u64; length.div_ceil(size_of::<u64>())];
        let allocated = storage.len() * size_of::<u64>();
        let mut returned = 0u32;
        // SAFETY: class 5 always returns SystemProcessInformation records. The
        // aligned allocation covers allocated bytes and returned is writable.
        let status = unsafe {
            query(
                5,
                storage.as_mut_ptr().cast(),
                allocated as u32,
                &mut returned,
            )
        };
        if status == LENGTH_MISMATCH {
            length = allocated.checked_mul(2)?.max(returned as usize);
            continue;
        }
        if status != 0 || returned == 0 || returned as usize > allocated {
            return None;
        }
        // SAFETY: the allocation is still alive and the API reports initialized
        // bytes within it. decode bounds-checks every record and never follows pointers.
        let bytes =
            unsafe { std::slice::from_raw_parts(storage.as_ptr().cast::<u8>(), returned as usize) };
        let records = decode(bytes)?;
        let pid = unsafe { GetCurrentProcessId() };
        if records.get(&pid)?.1 != super::process_birth(pid)? {
            return None;
        }
        return Some(records);
    }
    None
}

/// Supplement only processes continuously present across the existing Toolhelp
/// snapshot. An ABA/PID reuse, parent mismatch or unavailable query keeps unknown.
pub(super) fn supplement(
    processes: &mut super::Processes,
    before: Option<&Births>,
    after: Option<&Births>,
) {
    let (Some(before), Some(after)) = (before, after) else {
        return;
    };
    for (pid, process) in processes {
        if !process["created"].is_null() {
            continue;
        }
        let Some((parent, birth)) = before.get(pid) else {
            continue;
        };
        if after.get(pid) == Some(&(*parent, *birth))
            && process["ppid"].as_u64() == Some(u64::from(*parent))
        {
            process["created"] = serde_json::json!(birth);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn record(pid: usize, parent: usize, created: i64, next: u32) -> Vec<u8> {
        let mut data = vec![0; size_of::<Header>()];
        for (offset, bytes) in [
            (
                std::mem::offset_of!(Header, pid),
                pid.to_ne_bytes().to_vec(),
            ),
            (
                std::mem::offset_of!(Header, parent),
                parent.to_ne_bytes().to_vec(),
            ),
            (
                std::mem::offset_of!(Header, created),
                created.to_ne_bytes().to_vec(),
            ),
            (
                std::mem::offset_of!(Header, next),
                next.to_ne_bytes().to_vec(),
            ),
        ] {
            data[offset..offset + bytes.len()].copy_from_slice(&bytes);
        }
        data
    }

    #[test]
    fn parses_bounded_identity_rows_and_rejects_bad_layout() {
        let stride = size_of::<Header>() as u32;
        let mut bytes = record(10, 2, 100, stride);
        bytes.extend(record(11, 10, 101, 0));
        assert_eq!(
            decode(&bytes).unwrap(),
            Births::from([(10, (2, 100)), (11, (10, 101))])
        );
        assert!(decode(&bytes[..bytes.len() - 1]).is_none());
        assert!(decode(&record(10, 2, 100, 1)).is_none());
        assert!(decode(&record(10, 2, 100, u32::MAX)).is_none());
        let mut duplicate = record(10, 2, 100, stride);
        duplicate.extend(record(10, 2, 100, 0));
        assert!(decode(&duplicate).is_none());
    }

    #[test]
    fn denied_handle_birth_is_supplemented_only_across_a_stable_identity() {
        let before = Births::from([(10, (2, 100))]);
        for change in ["stable", "reused", "parent", "missing", "known", "failed"] {
            let mut ps = super::super::Processes::from([(
                10,
                json!({"pid":10,"ppid":2,"created":null,"cpu":null,"rss":0,"metrics_ok":false}),
            )]);
            let mut after = before.clone();
            match change {
                "reused" => {
                    after.insert(10, (2, 101));
                }
                "parent" => ps.get_mut(&10).unwrap()["ppid"] = json!(3),
                "missing" => after.clear(),
                "known" => ps.get_mut(&10).unwrap()["created"] = json!(200),
                _ => (),
            }
            supplement(
                &mut ps,
                Some(&before),
                (change != "failed").then_some(&after),
            );
            let expected = match change {
                "stable" => json!(100),
                "known" => json!(200),
                _ => serde_json::Value::Null,
            };
            assert_eq!(ps[&10]["created"], expected, "{change}");
            assert_eq!(ps[&10]["cpu"], serde_json::Value::Null);
            assert_eq!(ps[&10]["metrics_ok"], false);
        }
    }

    #[test]
    fn actual_system_snapshot_matches_the_current_process_handle() {
        let pid = unsafe { windows_sys::Win32::System::Threading::GetCurrentProcessId() };
        let expected = super::super::process_birth(pid).expect("own process birth");
        let records = snapshot().expect("native system birth query");
        assert_eq!(records[&pid].1, expected);
    }
}
