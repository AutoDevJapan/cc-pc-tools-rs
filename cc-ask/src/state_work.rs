//! Serialize background decisions separately from short state commits.
//! A commit applies only changes since this worker's last checkpoint to fresh state.
use crate::core::*;
use serde_json::{Map, Value};
use std::{cell::RefCell, collections::BTreeSet, path::PathBuf, time::Duration};

thread_local! {
    static ACTIVE: RefCell<Option<(PathBuf, Value, bool)>> = const { RefCell::new(None) };
}

pub struct StateWork {
    _worker: ByteLock,
}
impl StateWork {
    pub fn begin(ctx: &Context) -> Result<Self> {
        Self::begin_on(ctx, "state-work.lock")
    }
    pub(crate) fn begin_on(ctx: &Context, lock_name: &str) -> Result<Self> {
        recovery_guard(ctx)?;
        if ACTIVE.with(|a| a.borrow().is_some()) {
            return Err("nested state worker is not supported".into());
        }
        let worker = ByteLock::acquire(&ctx.here.join(lock_name), Duration::from_secs(60))?;
        let state = {
            let _state = ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))?;
            ctx.load_fresh()?
        };
        ACTIVE.with(|a| {
            *a.borrow_mut() = Some((ctx.here.clone(), state, lock_name == "reply-work.lock"))
        });
        Ok(Self { _worker: worker })
    }
}
impl Drop for StateWork {
    fn drop(&mut self) {
        ACTIVE.with(|a| *a.borrow_mut() = None);
    }
}

pub(crate) fn save(ctx: &Context, next: &Value) -> Option<Result<()>> {
    ACTIVE.with(|a| {
        let mut active = a.borrow_mut();
        let (here, base, _) = active.as_mut()?;
        if *here != ctx.here {
            return None;
        }
        Some((|| {
            let _state = ByteLock::acquire(&ctx.here.join("state.lock"), Duration::from_secs(60))?;
            let current = ctx.load_fresh()?;
            let merged = merge(base, next, &current, "")?;
            atomic_json(&ctx.here.join("state.json"), &merged, 2)?;
            // Keep the worker's view as the base, not the merged disk view. Otherwise
            // its next checkpoint would delete concurrently appended records.
            *base = next.clone();
            Ok(())
        })())
    })
}

pub(crate) fn snapshot(ctx: &Context) -> Option<Value> {
    ACTIVE.with(|a| {
        a.borrow()
            .as_ref()
            .filter(|(h, _, _)| *h == ctx.here)
            .map(|(_, v, _)| v.clone())
    })
}

pub(crate) fn owns_reader(ctx: &Context) -> bool {
    ACTIVE.with(|a| {
        a.borrow()
            .as_ref()
            .is_some_and(|(h, _, reader)| *h == ctx.here && *reader)
    })
}

/// Refuse effects from a superseded question or reply receipt. Fresh appended
/// questions for other threads do not invalidate the worker's current decision.
pub fn check(ctx: &Context) -> Result<()> {
    recovery_guard(ctx)?;
    ACTIVE.with(|a| {
        let active = a.borrow();
        let Some((here, base, _)) = active.as_ref().filter(|(h, _, _)| *h == ctx.here) else {
            return Ok(());
        };
        let current = ctx.load_fresh()?;
        let old = base["questions"].as_array().ok_or("invalid questions")?;
        let fresh = current["questions"].as_array().ok_or("invalid questions")?;
        if old.iter().any(|q| !fresh.iter().any(|p| p == q))
            || fresh.iter().any(|q| {
                !old.iter().any(|p| generation(p, q))
                    && (old.iter().any(|p| p["thread_id"] == q["thread_id"])
                        || crate::routing::question_inbox_only(ctx, q))
            })
            || current["answer_receipts"] != base["answer_receipts"]
            || current["inbox_only_post_contexts"] != base["inbox_only_post_contexts"]
        {
            return Err(format!(
                "StateConflict: worker view changed in {}; retry from fresh state",
                here.display()
            ));
        }
        Ok(())
    })
}

pub fn recovery_guard(ctx: &Context) -> Result<()> {
    if ctx.here.join("question-recovery-required.json").exists() {
        return Err("QuestionStateRecoveryRequired: 質問台帳と本人回答の記録が失われています。原記録の復旧を待ち、再質問・担当起動・投稿を行わないでください。本人の判断待ちではありません。".into());
    }
    Ok(())
}

fn conflict() -> String {
    "StateConflict: concurrent state update; retry from fresh state".into()
}

fn generation(a: &Value, b: &Value) -> bool {
    [
        "thread_id",
        "after",
        "asked_at",
        "source",
        "question_message",
    ]
    .iter()
    .all(|k| a[*k] == b[*k])
}

fn merge(base: &Value, next: &Value, current: &Value, key: &str) -> Result<Value> {
    if next == base {
        return Ok(current.clone());
    }
    if current == base || next == current {
        return Ok(next.clone());
    }
    if key == "questions" {
        let (Some(b), Some(n), Some(c)) = (base.as_array(), next.as_array(), current.as_array())
        else {
            return Err(conflict());
        };
        if n.len() < b.len() || c.len() < b.len() {
            return Err(conflict());
        }
        let mut result = c.clone();
        let mut used = BTreeSet::new();
        for (i, original) in b.iter().enumerate() {
            let at = c
                .iter()
                .enumerate()
                .find(|(at, q)| !used.contains(at) && generation(original, q))
                .map(|(at, _)| at)
                .ok_or_else(conflict)?;
            used.insert(at);
            // Treat a question generation as one record. Do not combine a new
            // continuation with an independently consumed/closed generation.
            if n[i] != *original && c[at] != *original && n[i] != c[at] {
                return Err(conflict());
            }
            result[at] = if n[i] == *original {
                c[at].clone()
            } else {
                n[i].clone()
            };
        }
        for q in n.iter().skip(b.len()) {
            // Foreground delivery can create this thread's first generation
            // while the worker is awaiting HTTP. Never merge two open generations.
            if c.iter().any(|p| {
                p["thread_id"] == q["thread_id"]
                    && !b.iter().any(|old| generation(old, p))
                    && !generation(p, q)
            }) {
                return Err(conflict());
            }
            if let Some(prior) = result.iter().find(|p| generation(p, q)) {
                if prior != q {
                    return Err(conflict());
                }
            } else {
                result.push(q.clone());
            }
        }
        return Ok(Value::Array(result));
    }
    if key == "receipt" {
        return Err(conflict());
    }
    if let (Some(b), Some(n), Some(c)) = (base.as_object(), next.as_object(), current.as_object()) {
        let keys: BTreeSet<_> = b.keys().chain(n.keys()).chain(c.keys()).collect();
        let mut out = Map::new();
        for k in keys {
            let (bv, nv, cv) = (b.get(k), n.get(k), c.get(k));
            let value = if nv == bv {
                cv.cloned()
            } else if cv == bv || nv == cv {
                nv.cloned()
            } else if let (Some(bv), Some(nv), Some(cv)) = (bv, nv, cv) {
                Some(merge(
                    bv,
                    nv,
                    cv,
                    if key == "answer_receipts" {
                        "receipt"
                    } else {
                        k
                    },
                )?)
            } else {
                return Err(conflict());
            };
            if let Some(v) = value {
                out.insert(k.clone(), v);
            }
        }
        return Ok(Value::Object(out));
    }
    Err(conflict())
}
