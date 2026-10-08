#![forbid(unsafe_code)]

use std::path::Path;

/// Quote a path as a literal PowerShell string, including apostrophes.
/// The resulting expression is used with `-LiteralPath` to avoid glob expansion.
///
/// ```
/// use std::path::Path;
/// use safe_delete_recovery::powershell_literal;
/// assert_eq!(powershell_literal(Path::new("C:/trial/a'b[1]")), "'C:/trial/a''b[1]'");
/// ```
pub fn powershell_literal(path: &Path) -> String {
    format!("'{}'", path.to_string_lossy().replace('\'', "''"))
}
