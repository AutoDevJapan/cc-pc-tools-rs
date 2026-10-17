package main

import (
	"bytes"
	"errors"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

var releaseGHAExpression = regexp.MustCompile(`\$\{\{[\s\S]*?\}\}`)
var releaseSecretRoot = regexp.MustCompile(`(?i)\bsecrets\b`)
var releaseTokenRoot = regexp.MustCompile(`(?i)\bgithub\s*(?:\.\s*token\b|\[\s*["']token["']\s*\])`)

type releaseDockerArgument struct {
	Kind, ASTSHA256, PublicFlag string
}

type releaseDockerCall struct {
	Line, ArgCount       int
	Operation            string
	ArgvASTSHA256        string
	Arguments            []releaseDockerArgument
	ComposeFileHashes    []string
	UnresolvedComposeRef bool
}

var releasePublicFlags = map[string]bool{
	"--rm": true, "--name": true, "--entrypoint": true, "--network": true,
	"--label": true, "--cpus": true, "--memory": true, "--pids-limit": true,
	"-e": true, "--env": true, "--env-file": true, "-p": true, "--publish": true,
	"-v": true, "--volume": true, "-w": true, "--workdir": true,
	"-d": true, "--detach": true, "-f": true, "--file": true,
	"--project-name": true, "--project-directory": true, "--profile": true,
	"--build": true, "--no-deps": true, "--remove-orphans": true,
	"--volumes": true, "--force-recreate": true, "--wait": true,
}

// A shell literal is not an expansion or command substitution. Never resolve
// product expressions with the owner's host environment.
func releaseLiteralWord(parts []syntax.WordPart) (string, bool) {
	var value strings.Builder
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			value.WriteString(p.Value)
		case *syntax.SglQuoted:
			value.WriteString(p.Value)
		case *syntax.DblQuoted:
			s, ok := releaseLiteralWord(p.Parts)
			if !ok {
				return "", false
			}
			value.WriteString(s)
		default:
			return "", false
		}
	}
	return value.String(), true
}

func releasePrintedNode(n syntax.Node) []byte {
	var b bytes.Buffer
	_ = syntax.NewPrinter().Print(&b, n)
	return b.Bytes()
}

func inspectReleaseShell(source string) ([]releaseDockerCall, int, error) {
	if len(source) > 1<<20 {
		return nil, 0, errors.New("release shell size rejected")
	}
	prefix := "__CC_GHA_" + digest([]byte(source))[:16] + "_"
	for strings.Contains(source, prefix) {
		prefix += "x"
	}
	n := 0
	prepared := releaseGHAExpression.ReplaceAllStringFunc(source, func(string) string {
		n++
		return prefix // Bind placeholders to the original whole-script digest.
	})
	ast, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(prepared), "")
	if err != nil {
		return nil, n, errors.New("release shell syntax unresolved")
	}
	calls := []releaseDockerCall{}
	syntax.Walk(ast, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		command, literal := releaseLiteralWord(call.Args[0].Parts)
		if !literal || command != "docker" {
			return true
		}
		c := releaseDockerCall{Line: int(call.Pos().Line()), ArgCount: len(call.Args), ArgvASTSHA256: digest(releasePrintedNode(call)), Operation: "unresolved"}
		literals := make([]string, len(call.Args))
		for i, arg := range call.Args {
			text, static := releaseLiteralWord(arg.Parts)
			a := releaseDockerArgument{Kind: "shell-expansion", ASTSHA256: digest(releasePrintedNode(arg))}
			if static && !strings.Contains(text, prefix) {
				a.Kind = "literal"
				literals[i] = text
				flag, _, _ := strings.Cut(text, "=")
				if releasePublicFlags[flag] {
					a.PublicFlag = flag // Never include the flag's value.
				}
			} else if strings.Contains(text, prefix) {
				a.Kind = "github-expression"
			}
			c.Arguments = append(c.Arguments, a)
		}
		if len(literals) > 1 {
			switch literals[1] {
			case "run", "build", "exec", "inspect", "logs", "ps", "rm", "wait", "network", "volume", "pull":
				c.Operation = "docker-" + literals[1]
			case "compose":
				c.Operation = "compose-unresolved"
				for i := 2; i < len(literals); i++ {
					x := literals[i]
					if x == "-f" || x == "--file" {
						i++
						if i < len(literals) && literals[i] != "" {
							c.ComposeFileHashes = append(c.ComposeFileHashes, digest([]byte(literals[i])))
						} else {
							c.UnresolvedComposeRef = true
						}
						continue
					}
					if strings.HasPrefix(x, "--file=") {
						c.ComposeFileHashes = append(c.ComposeFileHashes, digest([]byte(strings.TrimPrefix(x, "--file="))))
						continue
					}
					if x == "-p" || x == "--project-name" || x == "--project-directory" || x == "--profile" || x == "--env-file" {
						i++
						continue
					}
					if strings.HasPrefix(x, "--project-name=") || strings.HasPrefix(x, "--project-directory=") || strings.HasPrefix(x, "--profile=") || strings.HasPrefix(x, "--env-file=") {
						continue
					}
					switch x {
					case "up", "down", "run", "exec", "build", "logs", "ps", "config", "pull", "stop", "rm":
						c.Operation = "compose-" + x
					default:
						c.Operation = "compose-unresolved"
					}
					break
				}
				if len(c.ComposeFileHashes) == 0 {
					c.UnresolvedComposeRef = true // No implicit host COMPOSE_FILE lookup.
				}
			}
		}
		calls = append(calls, c)
		return true
	})
	return calls, n, nil
}
