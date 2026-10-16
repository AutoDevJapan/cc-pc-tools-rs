//! One kernel snapshot binds ancestry to birth times without opening foreign
//! processes. Only a later creation-checked retained handle authorizes a stop.
use std::{collections::HashSet, io};
use windows_sys::Win32::{
    Foundation::{CloseHandle, INVALID_HANDLE_VALUE},
    System::Diagnostics::ToolHelp::{
        CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, PROCESSENTRY32W,
        TH32CS_SNAPPROCESS,
    },
};

#[derive(Clone, Debug)]
pub(super) struct Entry {
    pub pid: u32,
    pub parent: u32,
    pub name: String,
    pub created: u64,
}

// Fixed prefix of SYSTEM_PROCESS_INFORMATION (SystemProcessInformation = 5).
// The SDK reserves the bytes preceding ImageName; PHNT names CreateTime:
// https://github.com/winsiderss/systeminformer/blob/master/phnt/include/ntexapi.h
// Keep the class/layout paired here, never accept a caller-selected layout.
#[repr(C)]
#[derive(Clone, Copy, Default)]
struct Header {
    next: u32,
    threads: u32,
    reserved: [u8; 24],
    created: u64,
    times: [u64; 2],
    name_len: u16,
    name_capacity: u16,
    name_buffer: usize,
    priority: i32,
    pid: usize,
    parent: usize,
}

#[link(name = "ntdll")]
unsafe extern "system" {
    fn NtQuerySystemInformation(
        class: i32,
        buffer: *mut std::ffi::c_void,
        length: u32,
        returned: *mut u32,
    ) -> i32;
}

fn invalid() -> io::Error {
    io::Error::other("invalid process identity snapshot")
}

fn documented_caller_parent() -> io::Result<u32> {
    // Parent is reserved in the public native layout. Cross-check this field
    // against documented ToolHelp before using native ancestry to stop a tree.
    unsafe {
        let snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
        if snapshot == INVALID_HANDLE_VALUE {
            return Err(io::Error::last_os_error());
        }
        let mut entry: PROCESSENTRY32W = std::mem::zeroed();
        entry.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;
        let mut available = Process32FirstW(snapshot, &mut entry);
        while available != 0 {
            if entry.th32ProcessID == std::process::id() {
                let parent = entry.th32ParentProcessID;
                CloseHandle(snapshot);
                return Ok(parent);
            }
            available = Process32NextW(snapshot, &mut entry);
        }
        CloseHandle(snapshot);
    }
    Err(io::Error::other("documented caller ancestry unavailable"))
}

fn validate_caller_parent(all: &[Entry], pid: u32, parent: u32) -> io::Result<()> {
    if all
        .iter()
        .any(|entry| entry.pid == pid && entry.parent == parent)
    {
        Ok(())
    } else {
        Err(io::Error::other(
            "native process ancestry layout disagrees with ToolHelp",
        ))
    }
}

fn parse(bytes: &[u8]) -> io::Result<Vec<Entry>> {
    let mut offset = 0usize;
    let mut result = Vec::new();
    let mut seen = HashSet::new();
    loop {
        let end = offset
            .checked_add(std::mem::size_of::<Header>())
            .ok_or_else(invalid)?;
        let header = bytes.get(offset..end).ok_or_else(invalid)?;
        // Header contains only integer fields, and the byte range is checked.
        let h = unsafe { header.as_ptr().cast::<Header>().read_unaligned() };
        let pid = u32::try_from(h.pid).map_err(|_| invalid())?;
        let parent = u32::try_from(h.parent).map_err(|_| invalid())?;
        if !seen.insert(pid) || h.name_len % 2 != 0 || h.name_len > h.name_capacity {
            return Err(invalid());
        }
        let name = if h.name_len == 0 {
            String::new()
        } else {
            // The kernel's UNICODE_STRING points into this same returned buffer.
            // Convert its address to checked offsets before reading any bytes.
            let start = h
                .name_buffer
                .checked_sub(bytes.as_ptr() as usize)
                .ok_or_else(invalid)?;
            let end = start.checked_add(h.name_len as usize).ok_or_else(invalid)?;
            let wide: Vec<_> = bytes
                .get(start..end)
                .ok_or_else(invalid)?
                .chunks_exact(2)
                .map(|pair| u16::from_ne_bytes([pair[0], pair[1]]))
                .collect();
            String::from_utf16_lossy(&wide).to_ascii_lowercase()
        };
        result.push(Entry {
            pid,
            parent,
            name,
            created: h.created,
        });
        if h.next == 0 {
            return Ok(result);
        }
        if (h.next as usize) < std::mem::size_of::<Header>() {
            return Err(invalid());
        }
        offset = offset.checked_add(h.next as usize).ok_or_else(invalid)?;
    }
}

pub(super) fn processes() -> io::Result<Vec<Entry>> {
    let mut size = 64 * 1024;
    for _ in 0..8 {
        // Native records require pointer alignment. Zeroing also makes a short
        // or malformed successful response safe to reject in the parser.
        let mut buffer = vec![0u64; size / 8];
        let mut returned = 0;
        // SAFETY: this writable aligned allocation has `size` bytes; class 5
        // returns SYSTEM_PROCESS_INFORMATION records, parsed only on success.
        let status = unsafe {
            NtQuerySystemInformation(5, buffer.as_mut_ptr().cast(), size as u32, &mut returned)
        };
        if status == 0 {
            if returned == 0 || returned as usize > size {
                return Err(invalid());
            }
            let bytes = unsafe {
                std::slice::from_raw_parts(buffer.as_ptr().cast::<u8>(), returned as usize)
            };
            let all = parse(bytes)?;
            validate_caller_parent(&all, std::process::id(), documented_caller_parent()?)?;
            return Ok(all);
        }
        if status != 0xc0000004u32 as i32 {
            // STATUS_INFO_LENGTH_MISMATCH
            return Err(io::Error::other(format!(
                "process identity snapshot NTSTATUS {status:#x}"
            )));
        }
        size = size
            .saturating_mul(2)
            .max(returned as usize)
            .next_multiple_of(8);
        if size > 32 * 1024 * 1024 {
            break;
        }
    }
    Err(io::Error::other(
        "process identity snapshot did not stabilize",
    ))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn native_snapshot_matches_retained_caller_identity() {
        assert_eq!(std::mem::offset_of!(Header, created), 32);
        assert_eq!(std::mem::offset_of!(Header, name_len), 56);
        #[cfg(target_pointer_width = "64")]
        assert_eq!(std::mem::offset_of!(Header, pid), 80);
        let pid = std::process::id();
        let created = crate::proc_created(pid).unwrap();
        let all = processes().unwrap();
        let me = all.iter().find(|p| p.pid == pid).unwrap();
        assert_eq!(me.created, created);
        assert_ne!(me.parent, 0);
        assert_eq!(me.parent, documented_caller_parent().unwrap());
        let mut wrong_parent = all.clone();
        wrong_parent
            .iter_mut()
            .find(|entry| entry.pid == pid)
            .unwrap()
            .parent ^= 1;
        assert!(validate_caller_parent(&wrong_parent, pid, me.parent).is_err());
        assert_eq!(
            me.name,
            std::env::current_exe()
                .unwrap()
                .file_name()
                .unwrap()
                .to_string_lossy()
                .to_ascii_lowercase()
        );
    }

    #[test]
    fn malformed_snapshot_is_rejected_before_dereferencing_names() {
        assert!(parse(&[]).is_err());
        let mut buffer = vec![0u64; std::mem::size_of::<Header>() / 8];
        for header in [
            Header {
                next: 1,
                ..Header::default()
            },
            Header {
                next: 4096,
                ..Header::default()
            },
            Header {
                name_len: 2,
                name_capacity: 2,
                name_buffer: 1,
                ..Header::default()
            },
            Header {
                name_len: 1,
                name_capacity: 1,
                ..Header::default()
            },
        ] {
            unsafe { buffer.as_mut_ptr().cast::<Header>().write_unaligned(header) };
            let bytes = unsafe {
                std::slice::from_raw_parts(
                    buffer.as_ptr().cast::<u8>(),
                    std::mem::size_of::<Header>(),
                )
            };
            assert!(parse(bytes).is_err());
        }
    }
}
