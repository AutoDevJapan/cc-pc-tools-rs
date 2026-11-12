package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
)

func (r *Runner) conversationNativePartial(ctx context.Context, opts conversationCLIOptions) (any, error) {
	if opts.Command == "native-implementation-record" {
		return r.nativePartialRecord(ctx, opts.InputFile)
	}
	if opts.Command == "check-native-implementation" {
		if !r.nativePartialGuard(ctx, opts.InputFile) {
			return nil, nativePartialFailed("input_guard")
		}
		b, err := generatedQuestionRead(opts.InputFile, 1<<20)
		if err != nil {
			return nil, errNativePartial
		}
		v, err := nativePartialDecode(b)
		if err != nil {
			return nil, err
		}
		state, root, err := r.nativePartialState(ctx, v)
		if err != nil {
			return nil, err
		}
		return r.nativePartialVerify(ctx, v, b, state, root)
	}
	// Rust calls this fixed, read-only verifier while it owns todo.lock. No
	// ordinary task CLI, write method, runner lock or cache update is allowed.
	if opts.Command != "verify-native-implementation" {
		return nil, errNativePartial
	}
	parent := filepath.Dir(opts.InputFile)
	if !regexp.MustCompile(`^[1-9][0-9]*-[a-f0-9]{64}\.native-partial\.json$`).MatchString(filepath.Base(opts.InputFile)) || filepath.Base(parent) != "replies" || filepath.Base(filepath.Dir(filepath.Dir(parent))) != "conversations" || !generatedWithin(filepath.Join(r.Config.Home, "conversations"), opts.InputFile) {
		return nil, errNativePartial
	}
	b, err := generatedQuestionRead(opts.InputFile, 1<<20)
	if err != nil {
		return nil, errNativePartial
	}
	v, err := nativePartialDecode(b)
	if err != nil || !conversationMediaSamePath(nativePartialFile(r, v, b), opts.InputFile) {
		return nil, errNativePartial
	}
	state, root, err := r.nativePartialState(ctx, v)
	if err != nil || !conversationMediaSamePath(opts.OriginStateFile, opts.InputFile+"-"+bytesHash(state)+".state.json") {
		return nil, errNativePartial
	}
	supplied, err := generatedQuestionRead(opts.OriginStateFile, maxTodo)
	var original any
	if err != nil || historyStrictJSON(supplied, &original) != nil || canonicalHash(json.RawMessage(supplied)) != canonicalHash(state) || bytesHash(supplied) != bytesHash(state) {
		return nil, errNativePartial
	}
	return r.nativePartialVerify(ctx, v, b, supplied, root)
}
