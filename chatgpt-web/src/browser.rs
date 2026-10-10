//! Production uses the existing owner Chrome extension. Isolated Chrome is restricted to file fixtures.
use crate::cdp;
use serde_json::{Value, json};
use std::{
    fs, io,
    path::{Path, PathBuf},
    time::Instant,
};

pub fn fixture_url(url: &str) -> bool {
    url.starts_with("file:///")
        || (std::env::var_os("CHATGPT_WEB_FIXTURE").as_deref() == Some(std::ffi::OsStr::new("1"))
            && loopback_fixture(url))
}

fn loopback_fixture(url: &str) -> bool {
    url.strip_prefix("http://127.0.0.1:").is_some_and(|rest| {
        rest.split('/')
            .next()
            .is_some_and(|port| port.parse::<u16>().is_ok_and(|p| p != 0))
    })
}

pub struct Browser {
    fixture: Option<cdp::Browser>,
    records: PathBuf,
    deadline: Instant,
}

impl Browser {
    pub fn open(home: &Path, url: &str, deadline: Instant) -> io::Result<Self> {
        let fixture = if fixture_url(url) {
            if std::env::var_os("CHATGPT_WEB_HEADLESS").as_deref()
                != Some(std::ffi::OsStr::new("1"))
            {
                return Err(io::Error::other(
                    "file fixtures require CHATGPT_WEB_HEADLESS=1",
                ));
            }
            Some(cdp::Browser::open(&home.join("profile"), false)?)
        } else {
            None
        };
        Ok(Self {
            fixture,
            records: home.join("owner-tabs"),
            deadline,
        })
    }

    pub fn new_tab(&mut self, url: &str, _: bool) -> io::Result<Tab> {
        if let Some(browser) = &mut self.fixture {
            if !fixture_url(url) {
                return Err(io::Error::other(
                    "fixture browser cannot navigate to a live service",
                ));
            }
            return Ok(Tab {
                cdp: Page::Fixture(Some(browser.new_tab(url, false)?)),
            });
        }
        #[cfg(windows)]
        {
            let record =
                cc_chrome::native::open_until(&self.records, "chatgpt-web", url, self.deadline)?;
            Ok(Tab {
                cdp: Page::Owner {
                    record,
                    records: self.records.clone(),
                    deadline: self.deadline,
                },
            })
        }
        #[cfg(not(windows))]
        Err(io::Error::other(
            "owner Chrome extension transport requires Windows",
        ))
    }

    pub fn close_tab(&mut self, mut tab: Tab) {
        if let Some(browser) = &mut self.fixture {
            if let Page::Fixture(tab) = &mut tab.cdp
                && let Some(tab) = tab.take()
            {
                browser.close_tab(tab);
            }
        } else {
            drop(tab);
        }
    }
}

pub struct Tab {
    pub cdp: Page,
}
pub enum Page {
    Fixture(Option<cdp::Tab>),
    #[cfg(windows)]
    Owner {
        record: cc_chrome::Record,
        records: PathBuf,
        deadline: Instant,
    },
}

impl Page {
    pub fn call(&mut self, method: &str, params: Value) -> io::Result<Value> {
        match self {
            Self::Fixture(tab) => tab
                .as_mut()
                .ok_or_else(|| io::Error::other("fixture tab closed"))?
                .cdp
                .call(method, params),
            #[cfg(windows)]
            Self::Owner {
                record, deadline, ..
            } => cc_chrome::native::page_command(record, method, params, *deadline),
        }
    }
    pub fn eval(&mut self, expr: &str) -> io::Result<Value> {
        if let Self::Fixture(tab) = self {
            return tab
                .as_mut()
                .ok_or_else(|| io::Error::other("fixture tab closed"))?
                .cdp
                .eval(expr);
        }
        let result = self.call(
            "Runtime.evaluate",
            json!({"expression":expr,"returnByValue":true,"awaitPromise":true}),
        )?;
        if result.get("exceptionDetails").is_some() {
            return Err(io::Error::other("page evaluation failed"));
        }
        Ok(result["result"]["value"].clone())
    }
}

#[cfg(windows)]
fn record_paths(records: &Path) -> io::Result<Vec<PathBuf>> {
    let mut paths = Vec::new();
    for folder in ["chrome-tabs", "chrome-emergency-tabs"] {
        let dir = records.join(folder);
        match fs::read_dir(dir) {
            Ok(entries) => {
                for entry in entries {
                    let path = entry?.path();
                    if path.extension().is_some_and(|e| e == "json") {
                        paths.push(path);
                    }
                }
            }
            Err(e) if e.kind() == io::ErrorKind::NotFound => {}
            Err(e) => return Err(e),
        }
    }
    Ok(paths)
}

#[cfg(windows)]
fn retire(records: &Path, record: &cc_chrome::Record) -> io::Result<()> {
    for path in record_paths(records)? {
        let saved: cc_chrome::Record = serde_json::from_slice(&fs::read(&path)?)?;
        if &saved == record {
            fs::remove_file(path)?;
        }
    }
    Ok(())
}

// Closing is coupled to tab ownership, including early check/probe/ask errors.
impl Drop for Tab {
    fn drop(&mut self) {
        #[cfg(windows)]
        if let Page::Owner {
            record, records, ..
        } = &mut self.cdp
        {
            match cc_chrome::native::close_page(record) {
                Ok(_) => {
                    if let Err(e) = retire(records, record) {
                        eprintln!("chatgpt_web: could not retire closed tab record: {e}");
                    }
                }
                Err(e) => eprintln!("chatgpt_web: own tab remains recorded for cleanup retry: {e}"),
            }
        }
    }
}

impl Drop for Browser {
    fn drop(&mut self) {
        if self.fixture.is_none() {
            // A timeout can occur after createTab was persisted but before Tab was returned.
            if let Err(e) = cleanup_records(&self.records) {
                eprintln!("chatgpt_web: own tab cleanup remains pending: {e}");
            }
        }
    }
}

fn cleanup_records(records: &Path) -> io::Result<()> {
    #[cfg(windows)]
    for path in record_paths(records)? {
        // A preceding retire may already have removed a duplicate emergency record.
        let bytes = match fs::read(&path) {
            Ok(bytes) => bytes,
            Err(e) if e.kind() == io::ErrorKind::NotFound => continue,
            Err(e) => return Err(e),
        };
        let record: cc_chrome::Record = serde_json::from_slice(&bytes)?;
        if record.job != "chatgpt-web" {
            return Err(io::Error::other("unexpected tab owner"));
        }
        cc_chrome::native::close_page(&record)?;
        retire(records, &record)?;
    }
    #[cfg(not(windows))]
    let _ = records;
    Ok(())
}

/// Caller holds the CLI OS lock, so no other CLI call owns these records.
pub fn cleanup(home: &Path) -> io::Result<()> {
    cleanup_records(&home.join("owner-tabs"))
}
#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;
    #[cfg(windows)]
    #[test]
    fn emergency_only_records_are_not_lost_when_normal_folder_is_absent() {
        let temp = tempfile::tempdir().unwrap();
        let records = temp.path().join("owner-tabs");
        fs::create_dir_all(records.join("chrome-emergency-tabs")).unwrap();
        let record = cc_chrome::Record {
            job: "chatgpt-web".into(),
            pipe: "test".into(),
            instance: "instance".into(),
            generation: "generation".into(),
            session: "session".into(),
            turn: "turn".into(),
            tab: 42,
            created: 0,
        };
        fs::write(
            records.join("chrome-emergency-tabs/record.json"),
            serde_json::to_vec(&record).unwrap(),
        )
        .unwrap();
        assert_eq!(record_paths(&records).unwrap().len(), 1);
        retire(&records, &record).unwrap();
        assert!(record_paths(&records).unwrap().is_empty());
    }
    #[test]
    fn live_urls_never_use_fixture_backend() {
        for url in [
            "https://chatgpt.com/",
            "http://127.0.0.1/",
            "https://example.com/file:///x",
        ] {
            assert!(!fixture_url(url));
        }
        assert!(fixture_url("file:///C:/fixture.html"));
        for url in [
            "http://127.0.0.1:80.evil/",
            "http://127.0.0.1:80@evil/",
            "http://127.0.0.1:0/",
            "https://chatgpt.com/",
        ] {
            assert!(!loopback_fixture(url));
        }
        assert!(loopback_fixture("http://127.0.0.1:1234/fixture"));
    }
    #[test]
    fn expired_owner_deadline_does_not_create_a_tab() {
        let temp = tempfile::tempdir().unwrap();
        let mut browser = Browser::open(
            temp.path(),
            "https://chatgpt.com/",
            Instant::now() - Duration::from_secs(1),
        )
        .unwrap();
        assert!(browser.new_tab("https://chatgpt.com/", false).is_err());
        assert!(!temp.path().join("owner-tabs").exists());
    }
}
