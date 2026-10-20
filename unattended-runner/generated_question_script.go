package main

// This is a deliberately small generation protocol, not a JavaScript engine.
// Every emitted image must originate in an actual tools.image_gen__imagegen
// return value. Unknown syntax fails closed rather than being guessed safe.
import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type generatedJSToken struct {
	text    string
	literal bool
}

func generatedJSTokens(code string) ([]generatedJSToken, error) {
	if len(code) > 128<<10 {
		return nil, errConversationMedia
	}
	var out []generatedJSToken
	for i := 0; i < len(code); {
		c := code[i]
		if strings.ContainsRune(" \r\n\t", rune(c)) {
			i++
			continue
		}
		if strings.HasPrefix(code[i:], "//") {
			n := strings.IndexFunc(code[i:], func(r rune) bool { return r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' })
			if n < 0 {
				break
			}
			_, width := utf8.DecodeRuneInString(code[i+n:])
			i += n + width
			continue
		}
		if strings.HasPrefix(code[i:], "/*") {
			n := strings.Index(code[i+2:], "*/")
			if n < 0 {
				return nil, errConversationMedia
			}
			i += n + 4
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			i++
			var value []byte
			closed := false
			for i < len(code) {
				c = code[i]
				i++
				if c == quote {
					closed = true
					break
				}
				if c < 32 {
					return nil, errConversationMedia
				}
				if c != '\\' {
					value = append(value, c)
					continue
				}
				if i >= len(code) {
					return nil, errConversationMedia
				}
				c = code[i]
				i++
				switch c {
				case '\\', '\'', '"', '/':
					value = append(value, c)
				case 'n':
					value = append(value, '\n')
				case 'r':
					value = append(value, '\r')
				case 't':
					value = append(value, '\t')
				case 'b':
					value = append(value, '\b')
				case 'f':
					value = append(value, '\f')
				case 'u', 'x':
					n := 4
					if c == 'x' {
						n = 2
					}
					if i+n > len(code) {
						return nil, errConversationMedia
					}
					v, e := strconv.ParseUint(code[i:i+n], 16, 32)
					if e != nil || (v >= 0xd800 && v <= 0xdfff) {
						return nil, errConversationMedia
					}
					value = utf8.AppendRune(value, rune(v))
					i += n
				default:
					return nil, errConversationMedia
				}
			}
			if !closed || !utf8.Valid(value) {
				return nil, errConversationMedia
			}
			out = append(out, generatedJSToken{string(value), true})
			continue
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '$' {
			start := i
			i++
			for i < len(code) {
				c = code[i]
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$') {
					break
				}
				i++
			}
			out = append(out, generatedJSToken{text: code[start:i]})
			continue
		}
		if strings.HasPrefix(code[i:], "===") {
			out = append(out, generatedJSToken{text: "==="})
			i += 3
			continue
		}
		if strings.ContainsRune("(){}[],:;.=", rune(c)) {
			out = append(out, generatedJSToken{text: string(c)})
			i++
			continue
		}
		return nil, errConversationMedia
	}
	if len(out) > 25000 {
		return nil, errConversationMedia
	}
	return out, nil
}

type generatedJSValue struct {
	kind    string
	ordinal int
}
type generatedJSParser struct {
	tokens    []generatedJSToken
	at, calls int
	vars      map[string]generatedJSValue
	emitted   []int
}

func (p *generatedJSParser) take(text string) bool {
	if p.at < len(p.tokens) && !p.tokens[p.at].literal && p.tokens[p.at].text == text {
		p.at++
		return true
	}
	return false
}
func (p *generatedJSParser) sequence(parts ...string) bool {
	for _, part := range parts {
		if !p.take(part) {
			return false
		}
	}
	return true
}
func (p *generatedJSParser) literal() (string, bool) {
	if p.at >= len(p.tokens) || !p.tokens[p.at].literal {
		return "", false
	}
	v := p.tokens[p.at].text
	p.at++
	return v, true
}
func (p *generatedJSParser) identifier() (string, bool) {
	if p.at >= len(p.tokens) || p.tokens[p.at].literal {
		return "", false
	}
	v := p.tokens[p.at].text
	if v == "" || !((v[0] >= 'a' && v[0] <= 'z') || (v[0] >= 'A' && v[0] <= 'Z') || v[0] == '_' || v[0] == '$') {
		return "", false
	}
	p.at++
	return v, true
}
func (p *generatedJSParser) declare(name string, value generatedJSValue) bool {
	if _, exists := p.vars[name]; exists {
		return false
	}
	if generatedJSReserved(name) {
		return false
	}
	p.vars[name] = value
	return true
}
func generatedJSReserved(name string) bool {
	for _, reserved := range []string{"tools", "Promise", "generatedImage", "load", "store", "yield_control", "text", "String", "const", "await", "for", "if", "else", "of", "true", "false"} {
		if name == reserved {
			return true
		}
	}
	return false
}
func (p *generatedJSParser) end() bool { return p.take(";") || p.at == len(p.tokens) }
func (p *generatedJSParser) prompt() bool {
	if _, ok := p.literal(); !ok {
		if p.take("load") {
			if !p.take("(") {
				return false
			}
			if _, ok = p.literal(); !ok || !p.take(")") {
				return false
			}
		} else {
			name, valid := p.identifier()
			if !valid || p.vars[name].kind != "prompt" {
				return false
			}
		}
	}
	for p.take(".") {
		method, ok := p.identifier()
		if !ok || (method != "replace" && method != "concat") || !p.take("(") {
			return false
		}
		if _, ok = p.literal(); !ok {
			return false
		}
		if method == "replace" {
			if !p.take(",") {
				return false
			}
			if _, ok = p.literal(); !ok {
				return false
			}
		}
		if !p.take(")") {
			return false
		}
	}
	return true
}
func (p *generatedJSParser) generation() bool {
	if !p.sequence("tools", ".", "image_gen__imagegen", "(", "{") {
		return false
	}
	seen := map[string]bool{}
	for {
		var key string
		var ok bool
		keyLiteral := false
		if key, ok = p.literal(); !ok {
			key, ok = p.identifier()
		} else {
			keyLiteral = true
		}
		if !ok || seen[key] {
			return false
		}
		seen[key] = true
		colon := p.take(":")
		switch key {
		case "prompt":
			if (colon && !p.prompt()) || (!colon && (keyLiteral || p.vars["prompt"].kind != "prompt")) {
				return false
			}
		case "transparent_background":
			if !colon || (!p.take("false") && !p.take("true")) {
				return false
			}
		default:
			return false
		}
		if p.take("}") {
			break
		}
		if !p.take(",") {
			return false
		}
	}
	return seen["prompt"] && p.take(")")
}
func (p *generatedJSParser) declaration() bool {
	if p.take("[") {
		var names []string
		for {
			name, ok := p.identifier()
			if !ok {
				return false
			}
			names = append(names, name)
			if p.take("]") {
				break
			}
			if !p.take(",") {
				return false
			}
		}
		if !p.sequence("=", "await", "Promise", ".", "allSettled", "(", "[") {
			return false
		}
		var values []generatedJSValue
		for {
			name, ok := p.identifier()
			if !ok || p.vars[name].kind != "promise" {
				return false
			}
			v := p.vars[name]
			v.kind = "settled"
			values = append(values, v)
			if p.take("]") {
				break
			}
			if !p.take(",") {
				return false
			}
		}
		if len(names) != len(values) || !p.sequence(")", ";") {
			return false
		}
		for i, name := range names {
			if !p.declare(name, values[i]) {
				return false
			}
		}
		return true
	}
	name, ok := p.identifier()
	if !ok || !p.take("=") {
		return false
	}
	await := p.take("await")
	if p.at < len(p.tokens) && !p.tokens[p.at].literal && p.tokens[p.at].text == "tools" {
		if !p.generation() {
			return false
		}
		kind := "promise"
		if await {
			kind = "result"
		}
		v := generatedJSValue{kind, p.calls}
		p.calls++
		return p.declare(name, v) && p.end()
	}
	return !await && p.prompt() && p.declare(name, generatedJSValue{kind: "prompt"}) && p.end()
}
func (p *generatedJSParser) store() bool {
	if !p.take("(") {
		return false
	}
	if _, ok := p.literal(); !ok || !p.take(",") {
		return false
	}
	name, ok := p.identifier()
	if !ok || (p.vars[name].kind != "prompt" && p.vars[name].kind != "result") {
		return false
	}
	return p.take(")") && p.end()
}
func (p *generatedJSParser) loop() bool {
	if !p.sequence("(", "const", "[") {
		return false
	}
	key, ok := p.identifier()
	if !ok || !p.take(",") {
		return false
	}
	result, ok := p.identifier()
	if !ok || key == result || generatedJSReserved(key) || generatedJSReserved(result) || !p.sequence("]", "of", "[") {
		return false
	}
	var ordinals []int
	for {
		if !p.take("[") {
			return false
		}
		if _, ok = p.literal(); !ok || !p.take(",") {
			return false
		}
		name, valid := p.identifier()
		if !valid || p.vars[name].kind != "settled" {
			return false
		}
		ordinals = append(ordinals, p.vars[name].ordinal)
		if !p.take("]") {
			return false
		}
		if p.take("]") {
			break
		}
		if !p.take(",") {
			return false
		}
	}
	if !p.sequence(")", "{", "if", "(", result, ".", "status", "===") {
		return false
	}
	status, ok := p.literal()
	if !ok || status != "fulfilled" || !p.sequence(")", "{") {
		return false
	}
	if p.take("store") {
		if !p.sequence("(", key, ",", result, ".", "value", ")", ";") {
			return false
		}
	}
	if !p.sequence("generatedImage", "(", result, ".", "value", ")", ";", "}") {
		return false
	}
	if p.take("else") {
		if !p.sequence("{", "text", "(", "{", key, ",", "error", ":", "String", "(", result, ".", "reason", ")", "}", ")", ";", "}") {
			return false
		}
	}
	if !p.take("}") {
		return false
	}
	p.emitted = append(p.emitted, ordinals...)
	return true
}
func validateGeneratedScript(code string, images int) error {
	tokens, err := generatedJSTokens(code)
	if err != nil {
		return err
	}
	p := generatedJSParser{tokens: tokens, vars: map[string]generatedJSValue{}}
	for p.at < len(p.tokens) {
		switch {
		case p.take("const"):
			if !p.declaration() {
				return errConversationMedia
			}
		case p.take("store"):
			if !p.store() {
				return errConversationMedia
			}
		case p.take("await"):
			if !p.sequence("yield_control", "(", ")", ";") {
				return errConversationMedia
			}
		case p.take("generatedImage"):
			if !p.take("(") {
				return errConversationMedia
			}
			name, ok := p.identifier()
			if !ok || p.vars[name].kind != "result" || !p.take(")") || !p.end() {
				return errConversationMedia
			}
			p.emitted = append(p.emitted, p.vars[name].ordinal)
		case p.take("for"):
			if !p.loop() {
				return errConversationMedia
			}
		default:
			return errConversationMedia
		}
	}
	if p.calls != images || len(p.emitted) != images {
		return errConversationMedia
	}
	for i, ordinal := range p.emitted {
		if i != ordinal {
			return errConversationMedia
		}
	}
	return nil
}
