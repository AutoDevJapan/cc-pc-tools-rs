//! Bounded input for the existing 3649 specification namespace only.
use std::{
    fs,
    io::{self, Read},
    path::Path,
};

fn plain(path: &Path) -> io::Result<()> {
    for part in path.ancestors() {
        let metadata = fs::symlink_metadata(part)?;
        #[cfg(windows)]
        let reparse = {
            use std::os::windows::fs::MetadataExt;
            metadata.file_attributes() & 0x400 != 0
        };
        #[cfg(not(windows))]
        let reparse = metadata.file_type().is_symlink();
        if reparse {
            return Err(io::Error::other(
                "registered review specification link refused",
            ));
        }
    }
    Ok(())
}

fn bounded(reader: impl Read) -> io::Result<Vec<u8>> {
    let mut bytes = Vec::new();
    reader.take(1024 * 1024 + 1).read_to_end(&mut bytes)?;
    if bytes.is_empty() || bytes.len() > 1024 * 1024 {
        return Err(io::Error::other(
            "registered review specification size refused",
        ));
    }
    Ok(bytes)
}

pub fn read(path: &Path) -> io::Result<Vec<u8>> {
    if !crate::unstarted_review_state::spec_path_matches(&path.to_string_lossy()) {
        return Err(io::Error::other(
            "registered review specification namespace refused",
        ));
    }
    plain(path)?;
    let file = fs::File::open(path)?;
    let metadata = file.metadata()?;
    if !metadata.is_file() || metadata.len() > 1024 * 1024 {
        return Err(io::Error::other("registered review specification refused"));
    }
    plain(path)?;
    // Same-user concurrent replacement is not an atomic filesystem attestation.
    // Acceptance still requires the exact immutable specification digest.
    bounded(file)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn bounded_spec_rejects_oversize_growth_and_incomplete_reads() {
        assert_eq!(
            bounded(vec![b'x'; 1024 * 1024].as_slice()).unwrap().len(),
            1024 * 1024
        );
        assert!(bounded(vec![b'x'; 1024 * 1024 + 1].as_slice()).is_err());
        assert!(bounded(&b""[..]).is_err());
        struct Broken;
        impl Read for Broken {
            fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
                Err(io::Error::other("read failure"))
            }
        }
        assert!(bounded(Broken).is_err());
    }
    #[test]
    fn unregistered_paths_are_rejected_before_any_filesystem_open() {
        for value in [
            "secret",
            "D:/CC/Tools/todo/specs/3255-00000000000000000000000000000000.md",
            "D:/CC/Tools/todo/specs/3649-../secret.md",
            "D:/CC/Tools/todo/specs/3649-00000000000000000000000000000000.md:stream",
        ] {
            let err = read(Path::new(value)).unwrap_err();
            assert_eq!(
                err.to_string(),
                "registered review specification namespace refused"
            );
        }
        assert!(crate::unstarted_review_state::spec_path_matches(
            "D:/CC/Tools/todo/specs/3649-00000000000000000000000000000000.md"
        ));
    }
}
