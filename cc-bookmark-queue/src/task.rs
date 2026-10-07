use crate::{ROOT, Result};
use std::{fs, path::Path, process::Command};

pub const TASK_NAME: &str = "CC-ChromeBookmarkQueue";
fn xml(s: &str) -> String {
    s.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&apos;")
}
pub fn task_xml(user: &str) -> String {
    format!(
        r#"<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>{}</UserId></LogonTrigger><TimeTrigger><Repetition><Interval>PT5M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition><StartBoundary>2026-10-07T00:00:00</StartBoundary><Enabled>true</Enabled></TimeTrigger></Triggers>
<Principals><Principal id="Author"><UserId>{}</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure></Settings>
<Actions Context="Author"><Exec><Command>wscript.exe</Command><Arguments>"D:\CC\Tools\chrome-bookmark-queue\run-hidden.vbs"</Arguments><WorkingDirectory>D:\CC\Tools\chrome-bookmark-queue</WorkingDirectory></Exec></Actions></Task>"#,
        xml(user),
        xml(user)
    )
}
pub fn hidden_launcher() -> &'static str {
    "Set sh = CreateObject(\"WScript.Shell\")\r\nrc = sh.Run(\"\"\"D:\\CC\\Tools\\chrome-bookmark-queue\\bookmark_queue.exe\"\" serve\", 0, True)\r\nWScript.Quit rc\r\n"
}
pub fn install(root: &Path) -> Result<()> {
    if root != Path::new(ROOT) {
        return Err("定期タスクは本番のキューフォルダにだけ登録できます".into());
    }
    let user = format!(
        "{}\\{}",
        std::env::var("USERDOMAIN")?,
        std::env::var("USERNAME")?
    );
    fs::write(root.join("run-hidden.vbs"), hidden_launcher())?;
    let text = task_xml(&user);
    let bytes: Vec<u8> = std::iter::once(0xfeff_u16)
        .chain(text.encode_utf16())
        .flat_map(u16::to_le_bytes)
        .collect();
    let path = root.join("task.xml");
    fs::write(&path, bytes)?;
    let mut command = Command::new("schtasks.exe");
    command
        .args(["/Create", "/TN", TASK_NAME, "/XML"])
        .arg(path);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000);
    }
    let status = command.status()?;
    if !status.success() {
        return Err("定期タスクの登録に失敗しました（既存タスクは上書きしません）".into());
    }
    let mut run = Command::new("schtasks.exe");
    run.args(["/Run", "/TN", TASK_NAME]);
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        run.creation_flags(0x08000000);
    }
    if !run.status()?.success() {
        return Err("定期タスクを起動できませんでした".into());
    }
    Ok(())
}
