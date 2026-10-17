package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// These types contain product source or paths. Even accidental fmt diagnostics
// must not bypass the JSON boundary's unexported fields.
func (sealedReleaseContext) Format(s fmt.State, _ rune) {
	io.WriteString(s, "<sealed-release-context>")
}
func (sealedReleaseFile) Format(s fmt.State, _ rune) { io.WriteString(s, "<sealed-release-file>") }
func (releaseTreeReply) Format(s fmt.State, _ rune)  { io.WriteString(s, "<release-tree>") }
func (releaseTreeEntry) Format(s fmt.State, _ rune)  { io.WriteString(s, "<release-tree-entry>") }
func (releaseBlobReply) Format(s fmt.State, _ rune)  { io.WriteString(s, "<release-blob-reply>") }
func (frozenReleaseBlob) Format(s fmt.State, _ rune) { io.WriteString(s, "<frozen-release-blob>") }

type releaseSourceReceipt struct {
	RepoID                           int64
	SourceSHA, Tree                  string
	SourceArchiveSHA256, InputSHA256 string
	Files, Directories, AuditedBlobs int
	FileBytes, InputBytes            int64
	ExecutionAuthorized              bool
	Workflows                        []releaseWorkflowInventory
}

func withMergedReleaseSource(ctx context.Context, read api, consume func(repository) error) error {
	r, err := verifyMerged(ctx, mergedCandidate(), read)
	if err != nil {
		return err
	}
	if err := consume(r); err != nil {
		return err
	}
	after, err := verifyMerged(ctx, mergedCandidate(), read)
	if err != nil {
		return err
	}
	if after != r {
		return errors.New("release repository changed during source read")
	}
	return nil
}

// Same distributed GitHub gate and authentication as all existing source reads.
// The caller constructs exactly one fixed tarball endpoint after verifyMerged.
// No CLI parameter selects the endpoint, ref, repository, or size limit.
func readReleaseSourceArchive(ctx context.Context, endpoint string) ([]byte, error) {
	return finiteRead(ctx, func() ([]byte, string, error) {
		c := cmd(ctx, gh, "api", endpoint)
		stderr := boundedBuffer{limit: 64 << 10}
		c.Stderr = &stderr
		b, err := boundedOutput(c, releaseContextLimit)
		return b, stderr.String(), err
	}, func() error { return releaseReadPause(ctx) })
}

func releaseReadPause(ctx context.Context) error {
	t := time.NewTimer(time.Second)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func inspectReleaseSource(ctx context.Context, read api, archiveRead func(context.Context, string) ([]byte, error)) (releaseSourceReceipt, error) {
	var report releaseSourceReceipt
	err := withMergedReleaseSource(ctx, read, func(r repository) error {
		var tree releaseTreeReply
		if err := read(ctx, "repos/"+r.FullName+"/git/trees/"+mergedTree+"?recursive=1", &tree); err != nil {
			return mergedReadFailure("release tree", err)
		}
		// Reject an invalid/truncated tree before requesting its source archive.
		if _, err := verifyReleaseTree(tree, mergedTree); err != nil {
			return err
		}
		raw, err := archiveRead(ctx, "repos/"+r.FullName+"/tarball/"+mergedSHA)
		if err != nil {
			return mergedReadFailure("release archive", err)
		}
		c, err := sealReleaseContext(tree, raw)
		if err != nil {
			return err
		}
		// Exercise the same verified reader a future builder must consume. Its
		// digest is checked, but no builder or product command is started here.
		input, err := c.verifiedArchive()
		if err != nil {
			return err
		}
		inputBytes, err := io.Copy(io.Discard, input)
		if err != nil {
			return errors.New("sealed release input read failed")
		}
		report = releaseSourceReceipt{RepoID: r.ID, SourceSHA: mergedSHA, Tree: c.tree, SourceArchiveSHA256: digest(raw), InputSHA256: c.archiveSHA256, Files: len(c.files), Directories: len(c.directories), AuditedBlobs: len(releaseBlobPins), InputBytes: inputBytes}
		for _, f := range c.files {
			report.FileBytes += int64(len(f.data))
		}
		// Parse only the verified snapshot. Do not read source blobs again or
		// choose mutable paths from an external report.
		for _, pin := range releaseBlobPins[:3] {
			for _, f := range c.files {
				if f.blob != pin.blob || f.sha256 != pin.sha256 {
					continue
				}
				w, err := inspectReleaseWorkflow(pin.role, f.data)
				if err != nil {
					return err
				}
				report.Workflows = append(report.Workflows, w)
				break
			}
		}
		return nil
	})
	if err != nil {
		return releaseSourceReceipt{}, err // Never publish a partial success.
	}
	return report, nil
}
