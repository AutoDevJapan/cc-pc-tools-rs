//! A missing record is not a wildcard. Prove the pinned invocation and home on
//! the held process handle. Raw command/environment bytes never leave this module.
use crate::{Config, Identity};
use std::mem::size_of;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};
use windows_sys::Wdk::System::Threading::{NtQueryInformationProcess, ProcessBasicInformation};
use windows_sys::Win32::Foundation::{HANDLE, LocalFree, WAIT_TIMEOUT};
use windows_sys::Win32::System::Diagnostics::Debug::ReadProcessMemory;
use windows_sys::Win32::System::Threading::{IsWow64Process2, WaitForSingleObject};
use windows_sys::Win32::UI::Shell::CommandLineToArgvW;

#[repr(C)]
#[derive(Default)]
struct Basic {
    exit: i32,
    peb: usize,
    affinity: usize,
    priority: i32,
    pid: usize,
    parent: usize,
}

fn read<const N: usize>(handle: HANDLE, address: usize) -> Result<[u8; N], String> {
    let mut bytes = [0; N];
    let mut returned = 0;
    if address == 0
        || address.checked_add(N).is_none()
        || unsafe {
            ReadProcessMemory(
                handle,
                address as *const _,
                bytes.as_mut_ptr().cast(),
                N,
                &mut returned,
            )
        } == 0
        || returned != N
    {
        return Err("owner_process_read".into());
    }
    Ok(bytes)
}

fn word(handle: HANDLE, address: usize) -> Result<usize, String> {
    Ok(usize::from_le_bytes(read(handle, address)?))
}

fn live(handle: HANDLE) -> Result<(), String> {
    if unsafe { WaitForSingleObject(handle, 0) } != WAIT_TIMEOUT {
        return Err("owner_process_not_live".into());
    }
    Ok(())
}

fn path_matches(actual: &Path, expected: &Path) -> bool {
    if !actual.is_absolute() {
        return false;
    }
    let normalize = |p: PathBuf| {
        p.to_string_lossy()
            .trim_start_matches("\\\\?\\")
            .replace('/', "\\")
            .to_ascii_lowercase()
    };
    match (
        std::fs::canonicalize(actual),
        std::fs::canonicalize(expected),
    ) {
        (Ok(actual), Ok(expected)) => normalize(actual) == normalize(expected),
        _ => false,
    }
}

fn launch_matches(args: &[String], config: &Config) -> bool {
    let Some(cli) = &config.expected_cli else {
        return false;
    };
    args.len() == 5
        && path_matches(Path::new(&args[0]), &config.expected_exe)
        && path_matches(Path::new(&args[1]), cli)
        && args[2] == "start"
        && args[3] == "--port"
        && args[4] == config.port.to_string()
}

fn command_args(command: &[u16]) -> Result<Vec<String>, String> {
    let mut terminated = command.to_vec();
    if terminated.contains(&0) {
        return Err("owner_command_invalid".into());
    }
    terminated.push(0);
    let mut count = 0;
    let raw = unsafe { CommandLineToArgvW(terminated.as_ptr(), &mut count) };
    if raw.is_null() {
        return Err("owner_command_decode".into());
    }
    let result = (|| {
        if !(1..=16).contains(&count) {
            return Err("owner_command_shape".into());
        }
        let mut args = Vec::new();
        for ptr in unsafe { std::slice::from_raw_parts(raw, count as usize) } {
            let mut len = 0;
            while len < terminated.len() && unsafe { *ptr.add(len) } != 0 {
                len += 1;
            }
            if len == terminated.len() {
                return Err("owner_command_shape".into());
            }
            args.push(
                String::from_utf16(unsafe { std::slice::from_raw_parts(*ptr, len) })
                    .map_err(|_| "owner_command_decode")?,
            );
        }
        Ok(args)
    })();
    unsafe {
        LocalFree(raw.cast());
    }
    result
}

fn home_matches(block: &[u16], expected: &Path) -> Result<bool, String> {
    if block.len() < 2 || !block.ends_with(&[0, 0]) {
        return Err("owner_environment_shape".into());
    }
    let mut home = None;
    let mut profile = None;
    for entry in block[..block.len() - 1]
        .split(|c| *c == 0)
        .filter(|entry| !entry.is_empty())
    {
        let text = String::from_utf16(entry).map_err(|_| "owner_environment_decode")?;
        let Some((name, value)) = text.split_once('=') else {
            return Err("owner_environment_shape".into());
        };
        let target = if name.eq_ignore_ascii_case("OPENCODEX_HOME") {
            &mut home
        } else if name.eq_ignore_ascii_case("USERPROFILE") {
            &mut profile
        } else {
            continue;
        };
        if target.replace(value.trim().to_owned()).is_some() {
            return Err("owner_environment_duplicate".into());
        }
    }
    let profile = profile.filter(|p| !p.is_empty()).map(PathBuf::from);
    let home = match home.filter(|p| !p.is_empty()) {
        Some(home) if home == "~" => profile,
        Some(home) if home.starts_with("~/") || home.starts_with("~\\") => {
            profile.map(|p| p.join(&home[2..]))
        }
        Some(home) => Some(PathBuf::from(home)),
        None => profile.map(|p| p.join(".opencodex")),
    };
    Ok(home.is_some_and(|p| path_matches(&p, expected)))
}

pub(crate) fn verify(handle: HANDLE, identity: Identity, config: &Config) -> Result<(), String> {
    if config.expected_cli.is_none() {
        return Err("owner_record_missing".into());
    }
    live(handle)?;
    if crate::windows::process_user_sid(handle)? != crate::windows::current_user_sid()? {
        return Err("owner_process_user".into());
    }
    let (mut process_machine, mut native_machine) = (0u16, 0u16);
    // The remote layout below is x64 only. A changed/unsupported ABI refuses proof.
    if size_of::<usize>() != 8
        || unsafe { IsWow64Process2(handle, &mut process_machine, &mut native_machine) } == 0
        || process_machine != 0
        || native_machine != 0x8664
    {
        return Err("owner_process_architecture".into());
    }
    let mut basic = Basic::default();
    let mut returned = 0;
    if unsafe {
        NtQueryInformationProcess(
            handle,
            ProcessBasicInformation,
            (&mut basic as *mut Basic).cast(),
            size_of::<Basic>() as u32,
            &mut returned,
        )
    } < 0
        || returned as usize != size_of::<Basic>()
        || basic.pid != identity.pid as usize
        || basic.peb == 0
    {
        return Err("owner_process_shape".into());
    }
    // PEB.ProcessParameters is the documented x64 prefix at 0x20. CommandLine
    // (0x70) and Environment (0x80) follow the x64 RTL_USER_PROCESS_PARAMETERS
    // prefix (Chromium Crashpad process_structs.h). Integration exercises this ABI.
    let params_slot = basic.peb.checked_add(0x20).ok_or("owner_process_address")?;
    let params = word(handle, params_slot)?;
    let prefix = read::<0x88>(handle, params)?;
    let flags = u32::from_le_bytes(prefix[8..12].try_into().unwrap());
    let len = u16::from_le_bytes(prefix[0x70..0x72].try_into().unwrap()) as usize;
    let max = u16::from_le_bytes(prefix[0x72..0x74].try_into().unwrap()) as usize;
    let command = usize::from_le_bytes(prefix[0x78..0x80].try_into().unwrap());
    let environment = usize::from_le_bytes(prefix[0x80..0x88].try_into().unwrap());
    if flags & 1 == 0
        || len == 0
        || len > max
        || !len.is_multiple_of(2)
        || command == 0
        || environment == 0
        || !command.is_multiple_of(2)
        || !environment.is_multiple_of(2)
    {
        return Err("owner_process_parameters".into());
    }
    let deadline = Instant::now() + Duration::from_secs(3);
    let mut command_text = Vec::new();
    for offset in (0..len).step_by(2) {
        if Instant::now() >= deadline {
            return Err("owner_process_deadline".into());
        }
        command_text.push(u16::from_le_bytes(read(
            handle,
            command.checked_add(offset).ok_or("owner_process_address")?,
        )?));
    }
    if !launch_matches(&command_args(&command_text)?, config) {
        return Err("owner_process_command".into());
    }
    let mut block = Vec::new();
    for offset in (0..65536).step_by(2) {
        if Instant::now() >= deadline {
            return Err("owner_process_deadline".into());
        }
        block.push(u16::from_le_bytes(read(
            handle,
            environment
                .checked_add(offset)
                .ok_or("owner_process_address")?,
        )?));
        if block.ends_with(&[0, 0]) {
            break;
        }
    }
    if !home_matches(&block, &config.home)? {
        return Err("owner_process_home".into());
    }
    // Detect pointer/prefix churn; this is a current observation, not an atomic
    // environment snapshot. Repeat the complete proof immediately before killing.
    if word(handle, params_slot)? != params || read::<0x88>(handle, params)? != prefix {
        return Err("owner_process_changed".into());
    }
    live(handle)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn environment_rejects_duplicate_truncated_and_relative_homes() {
        let block = |s: &str| s.encode_utf16().collect::<Vec<_>>();
        assert!(
            home_matches(
                &block("OPENCODEX_HOME=C:\\a\0opencodex_home=C:\\b\0\0"),
                Path::new("C:/a")
            )
            .is_err()
        );
        assert!(home_matches(&block("USERPROFILE=C:\\a\0"), Path::new("C:/a/.opencodex")).is_err());
        assert_eq!(
            home_matches(
                &block("OPENCODEX_HOME=.opencodex\0\0"),
                Path::new("C:/a/.opencodex")
            ),
            Ok(false)
        );
    }
}
