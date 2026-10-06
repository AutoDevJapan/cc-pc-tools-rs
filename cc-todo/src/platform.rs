use crate::Result;
use std::os::windows::{ffi::OsStrExt, io::AsRawHandle};
use std::{fs::File, path::Path};
use windows_sys::Win32::{
    Foundation::*,
    Networking::WinHttp::*,
    Storage::FileSystem::*,
    System::{IO::OVERLAPPED, Registry::*},
};
fn wide(s: &str) -> Vec<u16> {
    s.encode_utf16().chain(Some(0)).collect()
}
pub fn lock(file: &File) -> bool {
    let mut ov: OVERLAPPED = unsafe { std::mem::zeroed() };
    unsafe {
        LockFileEx(
            file.as_raw_handle(),
            LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY,
            0,
            1,
            0,
            &mut ov,
        ) != 0
    }
}
pub fn unlock(file: &File) {
    let mut ov: OVERLAPPED = unsafe { std::mem::zeroed() };
    unsafe {
        UnlockFileEx(file.as_raw_handle(), 0, 1, 0, &mut ov);
    }
}
pub fn replace(from: &Path, to: &Path) -> std::io::Result<()> {
    let from: Vec<u16> = from.as_os_str().encode_wide().chain(Some(0)).collect();
    let to: Vec<u16> = to.as_os_str().encode_wide().chain(Some(0)).collect();
    if unsafe { MoveFileExW(from.as_ptr(), to.as_ptr(), MOVEFILE_REPLACE_EXISTING) } == 0 {
        Err(std::io::Error::last_os_error())
    } else {
        Ok(())
    }
}
pub fn token() -> Result<String> {
    if let Ok(t) = std::env::var("GODD_DISCORD_BOT_TOKEN")
        && !t.is_empty()
    {
        return Ok(t);
    }
    let sub = wide("Environment");
    let name = wide("GODD_DISCORD_BOT_TOKEN");
    let mut size = 0;
    unsafe {
        if RegGetValueW(
            HKEY_CURRENT_USER,
            sub.as_ptr(),
            name.as_ptr(),
            RRF_RT_REG_SZ | RRF_RT_REG_EXPAND_SZ | RRF_NOEXPAND,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            &mut size,
        ) == ERROR_SUCCESS
        {
            let mut data = vec![0u16; (size as usize).div_ceil(2)];
            if RegGetValueW(
                HKEY_CURRENT_USER,
                sub.as_ptr(),
                name.as_ptr(),
                RRF_RT_REG_SZ | RRF_RT_REG_EXPAND_SZ | RRF_NOEXPAND,
                std::ptr::null_mut(),
                data.as_mut_ptr().cast(),
                &mut size,
            ) == ERROR_SUCCESS
            {
                let value = String::from_utf16_lossy(
                    &data[..data.iter().position(|c| *c == 0).unwrap_or(data.len())],
                );
                if !value.is_empty() {
                    return Ok(value);
                }
            }
        }
    }
    Err("GODD_DISCORD_BOT_TOKEN is not set".into())
}
struct Internet(*mut std::ffi::c_void);
impl Internet {
    fn new(handle: *mut std::ffi::c_void) -> Result<Self> {
        if handle.is_null() {
            Err("Discord HTTP request failed".into())
        } else {
            Ok(Self(handle))
        }
    }
}
impl Drop for Internet {
    fn drop(&mut self) {
        unsafe {
            WinHttpCloseHandle(self.0);
        }
    }
}
pub fn http(method: &str, path: &str, body: &[u8], token: &str) -> Result<(u32, Vec<u8>)> {
    let agent = wide("discord-ask (local, 1.0)");
    let host = wide("discord.com");
    let method = wide(method);
    let path = wide(&format!("/api/v10{path}"));
    let headers = wide(&format!(
        "Authorization: Bot {token}\r\nContent-Type: application/json\r\n"
    ));
    unsafe {
        let session = Internet::new(WinHttpOpen(
            agent.as_ptr(),
            WINHTTP_ACCESS_TYPE_DEFAULT_PROXY,
            std::ptr::null(),
            std::ptr::null(),
            0,
        ))?;
        WinHttpSetTimeouts(session.0, 20000, 20000, 20000, 20000);
        let connection = Internet::new(WinHttpConnect(session.0, host.as_ptr(), 443, 0))?;
        let request = Internet::new(WinHttpOpenRequest(
            connection.0,
            method.as_ptr(),
            path.as_ptr(),
            std::ptr::null(),
            std::ptr::null(),
            std::ptr::null(),
            WINHTTP_FLAG_SECURE,
        ))?;
        if WinHttpSendRequest(
            request.0,
            headers.as_ptr(),
            (headers.len() - 1) as u32,
            body.as_ptr().cast(),
            body.len() as u32,
            body.len() as u32,
            0,
        ) == 0
            || WinHttpReceiveResponse(request.0, std::ptr::null_mut()) == 0
        {
            return Err("Discord HTTP request failed".into());
        }
        let mut status: u32 = 0;
        let mut len = 4;
        if WinHttpQueryHeaders(
            request.0,
            WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
            std::ptr::null(),
            (&mut status as *mut u32).cast(),
            &mut len,
            std::ptr::null_mut(),
        ) == 0
        {
            return Err("Discord HTTP status unavailable".into());
        }
        let mut data = Vec::new();
        loop {
            let mut available = 0;
            if WinHttpQueryDataAvailable(request.0, &mut available) == 0 {
                return Err("Discord HTTP response failed".into());
            }
            if available == 0 {
                break;
            }
            let offset = data.len();
            data.resize(offset + available as usize, 0);
            let mut read = 0;
            if WinHttpReadData(
                request.0,
                data[offset..].as_mut_ptr().cast(),
                available,
                &mut read,
            ) == 0
            {
                return Err("Discord HTTP response failed".into());
            }
            data.truncate(offset + read as usize);
        }
        Ok((status, data))
    }
}
