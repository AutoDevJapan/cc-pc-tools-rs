use crate::{common::*, policy::*};
use serde_json::{json, Value};
use std::{
    mem::{size_of, zeroed},
    thread,
    time::Duration,
};
use windows_sys::Win32::{
    Foundation::{CloseHandle, FILETIME, INVALID_HANDLE_VALUE},
    Storage::FileSystem::GetDiskFreeSpaceExW,
    System::{
        Diagnostics::ToolHelp::{
            CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, PROCESSENTRY32W,
            TH32CS_SNAPPROCESS,
        },
        ProcessStatus::{
            GetPerformanceInfo, GetProcessMemoryInfo, PERFORMANCE_INFORMATION,
            PROCESS_MEMORY_COUNTERS_EX,
        },
        SystemInformation::{
            GetLogicalProcessorInformation, GetTickCount64, RelationProcessorCore,
            SYSTEM_LOGICAL_PROCESSOR_INFORMATION,
        },
        Threading::{
            GetProcessTimes, GetSystemTimes, OpenProcess, TerminateProcess,
            PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_TERMINATE,
        },
    },
};
fn ft(t: FILETIME) -> u64 {
    (t.dwHighDateTime as u64) << 32 | t.dwLowDateTime as u64
}
unsafe fn times(h: *mut std::ffi::c_void) -> Option<(u64, f64)> {
    let (mut birth, mut exit, mut kernel, mut user) = (zeroed(), zeroed(), zeroed(), zeroed());
    (GetProcessTimes(h, &mut birth, &mut exit, &mut kernel, &mut user) != 0)
        .then(|| (ft(birth), (ft(kernel) + ft(user)) as f64 / 1e7))
}
pub fn process_birth(pid: u32) -> Option<u64> {
    unsafe {
        let h = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
        if h.is_null() {
            return None;
        }
        let t = times(h).map(|x| x.0);
        CloseHandle(h);
        t
    }
}
pub fn system_times() -> Result<Vec<u64>> {
    unsafe {
        let (mut idle, mut kernel, mut user) = (zeroed(), zeroed(), zeroed());
        if GetSystemTimes(&mut idle, &mut kernel, &mut user) == 0 {
            return Err(Error("OSError"));
        }
        Ok(vec![ft(idle), ft(kernel), ft(user)])
    }
}
/// One Toolhelp snapshot, then metadata on birth-verified handles. No command lines.
pub fn process_snapshot() -> Result<Processes> {
    unsafe {
        let snap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
        if snap == INVALID_HANDLE_VALUE {
            return Err(Error("OSError"));
        }
        let mut row: PROCESSENTRY32W = zeroed();
        row.dwSize = size_of::<PROCESSENTRY32W>() as u32;
        let mut more = Process32FirstW(snap, &mut row);
        if more == 0 {
            CloseHandle(snap);
            return Err(Error("OSError"));
        }
        let mut ps = Processes::new();
        while more != 0 {
            let pid = row.th32ProcessID;
            let end = row.szExeFile.iter().position(|c| *c == 0).unwrap_or(260);
            let name = String::from_utf16_lossy(&row.szExeFile[..end]);
            let mut p = json!({"pid":pid,"ppid":row.th32ParentProcessID,"name":name,"created":null,"rss":0,"cpu":null,"metrics_ok":false});
            let h = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, 0, pid);
            if !h.is_null() {
                if let Some((birth, cpu)) = times(h) {
                    p["created"] = json!(birth);
                    p["cpu"] = json!(cpu);
                }
                let mut mem: PROCESS_MEMORY_COUNTERS_EX = zeroed();
                mem.cb = size_of::<PROCESS_MEMORY_COUNTERS_EX>() as u32;
                if GetProcessMemoryInfo(
                    h,
                    (&mut mem as *mut PROCESS_MEMORY_COUNTERS_EX).cast(),
                    mem.cb,
                ) != 0
                {
                    p["rss"] = json!(mem.WorkingSetSize);
                    p["metrics_ok"] = json!(!p["cpu"].is_null());
                }
                CloseHandle(h);
            }
            ps.insert(pid, p);
            more = Process32NextW(snap, &mut row);
        }
        CloseHandle(snap);
        Ok(ps)
    }
}
pub fn measure(state: &mut Value) -> Result<(Value, Processes)> {
    let ps = process_snapshot()?;
    unsafe {
        let mut info: PERFORMANCE_INFORMATION = zeroed();
        info.cb = size_of::<PERFORMANCE_INFORMATION>() as u32;
        if GetPerformanceInfo(&mut info, info.cb) == 0 || info.CommitLimit == 0 {
            return Err(Error("OSError"));
        }
        let sample = now();
        let cpu = if sample - n(&state["system_sample_s"]) < 15.
            && state.get("last_cpu_percent").is_some()
        {
            n(&state["last_cpu_percent"])
        } else {
            let mut current = system_times()?;
            let mut old: Vec<u64> = arr(&state["system_times"])
                .iter()
                .filter_map(Value::as_u64)
                .collect();
            if old.len() != 3 || current.iter().zip(&old).any(|(a, b)| a < b) {
                old = current;
                thread::sleep(Duration::from_millis(250));
                current = system_times()?;
            }
            let cpu = cpu_delta(&old, &current);
            merge(
                state,
                json!({"system_times":current,"system_sample_s":sample,"last_cpu_percent":cpu}),
            );
            cpu
        };
        let mut bytes = 0;
        GetLogicalProcessorInformation(std::ptr::null_mut(), &mut bytes);
        let mut data: Vec<SYSTEM_LOGICAL_PROCESSOR_INFORMATION> =
            vec![zeroed(); bytes as usize / size_of::<SYSTEM_LOGICAL_PROCESSOR_INFORMATION>()];
        if GetLogicalProcessorInformation(data.as_mut_ptr(), &mut bytes) == 0 {
            return Err(Error("OSError"));
        }
        let cores = data
            .iter()
            .filter(|x| x.Relationship == RelationProcessorCore)
            .count();
        let logical = std::thread::available_parallelism()
            .map(|n| n.get())
            .unwrap_or(1);
        let mut disks = json!({});
        for drive in ["C:", "D:"] {
            let path: Vec<u16> = format!("{drive}/").encode_utf16().chain(Some(0)).collect();
            let mut available = 0;
            let mut free = 0;
            if GetDiskFreeSpaceExW(
                path.as_ptr(),
                &mut available,
                std::ptr::null_mut(),
                &mut free,
            ) == 0
            {
                return Err(Error("OSError"));
            }
            disks[drive] = json!(free as f64 / GIB);
        }
        Ok((
            json!({"physical_cores":cores,"logical_cores":logical,"ram_total_gb":(info.PhysicalTotal*info.PageSize) as f64/GIB,"available_gb":(info.PhysicalAvailable*info.PageSize) as f64/GIB,"commit_gb":(info.CommitTotal*info.PageSize) as f64/GIB,"commit_limit_gb":(info.CommitLimit*info.PageSize) as f64/GIB,"commit_percent":info.CommitTotal as f64/info.CommitLimit as f64*100.,"cpu_percent":cpu,"boot_s":now()-GetTickCount64() as f64/1000.,"disk_free_gb":disks}),
            ps,
        ))
    }
}
pub fn kill_orphan(c: &Value) -> bool {
    unsafe {
        let h = OpenProcess(
            PROCESS_QUERY_LIMITED_INFORMATION | PROCESS_TERMINATE,
            0,
            n(&c["pid"]) as u32,
        );
        if h.is_null() {
            return false;
        }
        let killed = times(h).is_some_and(|(birth, _)| {
            Some(birth) == c["created"].as_u64() && TerminateProcess(h, 1) != 0
        });
        CloseHandle(h);
        killed
    }
}
