//! Update one existing task note under the same lock as the shared ToDo CLI.
fn main() -> Result<(), String> {
    let id = std::env::args()
        .nth(1)
        .ok_or("task ID required")?
        .parse::<i64>()
        .map_err(|e| e.to_string())?;
    let note = std::env::args().nth(2).ok_or("note required")?;
    let paths = cc_todo::Paths::production();
    let _lock = cc_todo::Lock::acquire(&paths.lock, std::time::Duration::from_secs(30))?;
    let mut db = cc_todo::load(&paths.store)?;
    let item = cc_todo::find(&mut db, id)?;
    item["note"] = serde_json::json!(note);
    item["updated"] = serde_json::json!(cc_todo::now());
    cc_todo::save(&paths.store, &db)?;
    println!("recorded task {id}");
    Ok(())
}
