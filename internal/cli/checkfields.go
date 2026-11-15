package cli

import (
	"encoding/json"
	"strings"
)

// checkFields is the lines a successful check prints for its selected
// answer: each selected field the answer carries, as name: value. A dotted
// entry is a path into the answer, which already holds only what it keeps,
// so its top-level key prints once, with the filtered value. A field the
// answer does not carry prints no line.
func checkFields(answer json.RawMessage, sel []string) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(answer, &obj) != nil {
		return nil
	}
	var lines []string
	printed := map[string]bool{}
	for _, name := range sel {
		top, _, _ := strings.Cut(name, ".")
		if printed[top] {
			continue
		}
		if raw, ok := obj[top]; ok {
			printed[top] = true
			lines = append(lines, top+": "+checkField(raw))
		}
	}
	return lines
}

// checkField renders one selected field of a check call's answer for
// its line: a string as it is, a number or a boolean as its JSON, and an
// object or a list as compact JSON, as the answer already holds it.
func checkField(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
