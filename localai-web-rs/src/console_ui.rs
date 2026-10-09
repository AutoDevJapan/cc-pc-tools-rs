//! Shared presentation for the Console and its existing music pages.
const SHELL: &str = include_str!("console_shell.html");
const MARKER: &str = "<meta name=\"local-ai-console-shell\" content=\"1\">";

/// Keep existing forms, CSRF values, routes, media and scripts intact.
pub fn decorate_html(html: &str) -> String {
    if html.contains(MARKER) {
        return html.to_owned();
    }
    let at = html
        .as_bytes()
        .windows(7)
        .rposition(|candidate| candidate.eq_ignore_ascii_case(b"</body>"))
        .unwrap_or(html.len());
    let mut output = String::with_capacity(html.len() + SHELL.len() + MARKER.len());
    output.push_str(&html[..at]);
    output.push_str(MARKER);
    output.push_str(SHELL);
    output.push_str(&html[at..]);
    output
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn preserves_music_form_and_media_and_decorates_only_once() {
        let html = "<!doctype html><BoDy><form action='/music/drafts' method='post'><input name='csrf' value='test-only'></form><audio src='/music/example/audio'></audio></BoDy>";
        let rendered = decorate_html(html);
        assert!(rendered.contains("<form action='/music/drafts' method='post'><input name='csrf' value='test-only'></form>"));
        assert!(rendered.contains("<audio src='/music/example/audio'></audio>"));
        assert!(rendered.ends_with("</BoDy>"));
        assert_eq!(rendered.matches(MARKER).count(), 1);
        assert_eq!(decorate_html(&rendered), rendered);
    }

    #[test]
    fn fragment_gets_shell_without_changing_existing_message() {
        let rendered = decorate_html("<h1>音楽制作</h1><p>接続を確認してください。</p>");
        assert!(rendered.starts_with("<h1>音楽制作</h1><p>接続を確認してください。</p>"));
        assert_eq!(rendered.matches(MARKER).count(), 1);
    }
}
