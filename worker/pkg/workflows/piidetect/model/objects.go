package model

import (
	"encoding/json"
	"regexp"
	"strings"
)

// The tags of a block in which a model writes its reasoning, opening or closing, in any
// case: the names between angle brackets, with or without attributes, and the bracketed
// form. The list is closed.
var reasoningTag = regexp.MustCompile(
	`(?i)<(/?)(?:think|thinking|thought|thoughts|reasoning|reflection|scratchpad|seed:think)(?:\s[^>]*)?>` +
		`|\[(/?)think\]`,
)

// outsideReasoning returns the pieces of the content that stand outside the reasoning
// blocks, in their order. Blocks may follow each other or hold one another. A block that
// is not closed runs to the end; a block that is closed without having been opened
// started at the beginning. reachesEnd tells that the last piece runs to the end of the
// content: no block follows it.
func outsideReasoning(content string) (pieces []string, reachesEnd bool) {
	depth, from := 0, 0
	for _, tag := range reasoningTag.FindAllStringSubmatchIndex(content, -1) {
		start, end := tag[0], tag[1]
		closing := tag[3] > tag[2] || tag[5] > tag[4]
		switch {
		case !closing:
			if depth == 0 {
				pieces = append(pieces, content[from:start])
			}
			depth++
		case depth > 0:
			depth--
		default:
			pieces = nil
		}
		from = end
	}
	if depth > 0 {
		return pieces, false
	}
	return append(pieces, content[from:]), true
}

// answerObject is a JSON object that may be the answer of the model.
type answerObject struct {
	// members are the values by key. A key written twice holds null: it answers nothing.
	members map[string]json.RawMessage
	// closes tells that nothing follows the object in the content but spaces or the end
	// of a code fence.
	closes bool
}

// What may follow the answer in a completion that ends with it.
var afterAnswer = regexp.MustCompile("^\\s*(?:```)?\\s*$")

// answerObjects finds, in their order, the JSON objects that decode whole and name at
// least one column of the request. An object inside braces that are open — those of
// another object, whole, cut or malformed — is not one of them.
func answerObjects(pieces []string, reachesEnd bool, asked map[string]bool) []answerObject {
	var objects []answerObject
	for i, piece := range pieces {
		last := reachesEnd && i == len(pieces)-1
		for at := 0; at < len(piece); {
			open := strings.IndexByte(piece[at:], '{')
			if open < 0 {
				break
			}
			at += open
			members, size, ok := decodeObject(piece[at:])
			if !ok {
				at += bracesEnd(piece[at:])
				continue
			}
			at += size
			if namesAColumn(members, asked) {
				objects = append(objects, answerObject{
					members: members,
					closes:  last && afterAnswer.MatchString(piece[at:]),
				})
			}
		}
	}
	return objects
}

// decodeObject reads the JSON object a text starts with: its members by key, and its
// size. A key written twice holds null.
func decodeObject(text string) (members map[string]json.RawMessage, size int, ok bool) {
	decoder := json.NewDecoder(strings.NewReader(text))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, 0, false
	}
	members = map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		key, isKey := token.(string)
		if err != nil || !isKey {
			return nil, 0, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, 0, false
		}
		if _, twice := members[key]; twice {
			value = json.RawMessage("null")
		}
		members[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, 0, false
	}
	return members, int(decoder.InputOffset()), true
}

// bracesEnd returns where the braces a text starts with are closed, counting those they
// hold and leaving aside the ones inside a quoted text; the length of the text when they
// are not closed.
func bracesEnd(text string) int {
	depth, quoted, escaped := 0, false, false
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case escaped:
			escaped = false
		case quoted:
			escaped = c == '\\'
			quoted = c != '"'
		case c == '"':
			quoted = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(text)
}

func namesAColumn(members map[string]json.RawMessage, asked map[string]bool) bool {
	for id := range members {
		if asked[id] {
			return true
		}
	}
	return false
}
