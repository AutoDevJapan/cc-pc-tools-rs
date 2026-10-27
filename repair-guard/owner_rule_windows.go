package main

import "context"

type ownerRuleConfig struct {
	dev string
	infrastructure []string
	records []string
}

func ownerRuleWith(ctx context.Context, path string, projects object, access ownerSourceAccess, config ownerRuleConfig, owned func(string) bool) ownerPathVerdict {
	return ownerUnverified()
}
