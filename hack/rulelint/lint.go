package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/k8sstormcenter/sbob-go/rules"
	"sigs.k8s.io/yaml"
)

const tagPrefix = "portability:"

var acceptedNotApplicable = map[string]bool{
	"constant expression true": true,
	"no expressions":           true,
}

var actionPattern = regexp.MustCompile(`(?s)\{\{.*?\}\}`)

type ruleExpression struct {
	EventType  string `json:"eventType"`
	Expression string `json:"expression"`
}

type chartRule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Tags        []string `json:"tags"`
	Expressions struct {
		RuleExpression []ruleExpression `json:"ruleExpression"`
	} `json:"expressions"`
}

type rulesDocument struct {
	Spec struct {
		Rules []chartRule `json:"rules"`
	} `json:"spec"`
}

type result struct {
	ID       string
	Name     string
	Class    rules.Class
	Reasons  []string
	Problems []string
}

type span struct {
	start  int
	end    int
	indent int
}

func stripActions(src string) string {
	return actionPattern.ReplaceAllStringFunc(src, func(action string) string {
		return strings.Repeat("\n", strings.Count(action, "\n"))
	})
}

func parseRules(src string) ([]chartRule, error) {
	var doc rulesDocument
	if err := yaml.Unmarshal([]byte(stripActions(src)), &doc); err != nil {
		return nil, err
	}
	return doc.Spec.Rules, nil
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func isBlank(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

func isItem(line string) bool {
	t := strings.TrimSpace(line)
	return t == "-" || strings.HasPrefix(t, "- ")
}

func ruleSpans(lines []string) ([]span, error) {
	head := -1
	inSpec := false
	for i, line := range lines {
		if isBlank(line) {
			continue
		}
		if indentOf(line) == 0 {
			inSpec = strings.TrimSpace(line) == "spec:"
			continue
		}
		if inSpec && strings.TrimSpace(line) == "rules:" {
			head = i
			break
		}
	}
	if head < 0 {
		return nil, fmt.Errorf("no spec.rules list found")
	}
	var spans []span
	indent := -1
	for i := head + 1; i < len(lines); i++ {
		line := lines[i]
		if isBlank(line) {
			continue
		}
		ind := indentOf(line)
		if indent < 0 {
			if !isItem(line) || ind < indentOf(lines[head]) {
				break
			}
			indent = ind
		}
		if ind < indent || (ind == indent && !isItem(line)) {
			break
		}
		if ind == indent {
			spans = append(spans, span{start: i, end: i + 1, indent: indent})
			continue
		}
		spans[len(spans)-1].end = i + 1
	}
	return spans, nil
}

func keyText(line string, first bool, keyIndent int) string {
	if first {
		line = strings.Replace(line, "- ", "  ", 1)
	}
	if indentOf(line) != keyIndent {
		return ""
	}
	return strings.TrimSpace(line)
}

func tagValue(line string) string {
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	return v
}

func tagLine(indent int, class rules.Class) string {
	return strings.Repeat(" ", indent) + `- "` + tagPrefix + string(class) + `"`
}

func insertAt(lines []string, at int, added ...string) []string {
	out := make([]string, 0, len(lines)+len(added))
	out = append(out, lines[:at]...)
	out = append(out, added...)
	return append(out, lines[at:]...)
}

func setTag(lines []string, sp span, class rules.Class) ([]string, bool, error) {
	keyIndent := sp.indent + 2
	tagsAt := -1
	for i := sp.start; i < sp.end; i++ {
		text := keyText(lines[i], i == sp.start, keyIndent)
		if text == "tags:" || strings.HasPrefix(text, "tags: ") || strings.HasPrefix(text, "tags:\t") {
			tagsAt = i
			break
		}
	}
	if tagsAt < 0 {
		return insertAt(lines, sp.end, strings.Repeat(" ", keyIndent)+"tags:", tagLine(keyIndent+2, class)), true, nil
	}
	at := strings.Index(lines[tagsAt], "tags:")
	value := strings.TrimSpace(lines[tagsAt][at+len("tags:"):])
	if value == "[]" {
		out := append([]string{}, lines...)
		out[tagsAt] = lines[tagsAt][:at] + "tags:"
		return insertAt(out, tagsAt+1, tagLine(keyIndent+2, class)), true, nil
	}
	if value != "" && !strings.HasPrefix(value, "#") {
		return nil, false, fmt.Errorf("line %d: tags must be a block list", tagsAt+1)
	}
	last := -1
	found := -1
	for i := tagsAt + 1; i < sp.end; i++ {
		if isBlank(lines[i]) {
			continue
		}
		if indentOf(lines[i]) < keyIndent || !isItem(lines[i]) {
			break
		}
		last = i
		if strings.HasPrefix(tagValue(lines[i]), tagPrefix) {
			if found >= 0 {
				return nil, false, fmt.Errorf("line %d: more than one %s tag", i+1, strings.TrimSuffix(tagPrefix, ":"))
			}
			found = i
		}
	}
	if found >= 0 {
		want := tagLine(indentOf(lines[found]), class)
		if lines[found] == want {
			return lines, false, nil
		}
		out := append([]string{}, lines...)
		out[found] = want
		return out, true, nil
	}
	if last < 0 {
		return insertAt(lines, tagsAt+1, tagLine(keyIndent+2, class)), true, nil
	}
	return insertAt(lines, last+1, tagLine(indentOf(lines[last]), class)), true, nil
}

func checkTag(tags []string, class rules.Class) string {
	var have []string
	for _, t := range tags {
		if strings.HasPrefix(t, tagPrefix) {
			have = append(have, t)
		}
	}
	want := tagPrefix + string(class)
	switch {
	case len(have) == 0:
		return "tag " + want + " is missing"
	case len(have) > 1:
		return "more than one " + strings.TrimSuffix(tagPrefix, ":") + " tag: " + strings.Join(have, ", ")
	case have[0] != want:
		return "tag " + have[0] + " is stale, want " + want
	}
	return ""
}

func classify(r chartRule) result {
	res := result{ID: r.ID, Name: r.Name}
	if entry, ok := rules.ByEngineID(r.ID); ok {
		res.Name = entry.Name
	} else if r.ID == "" {
		res.Problems = append(res.Problems, "rule has no id")
	} else {
		res.Problems = append(res.Problems, "id is not in the catalogue")
	}
	expressions := make([]string, 0, len(r.Expressions.RuleExpression))
	for _, e := range r.Expressions.RuleExpression {
		expressions = append(expressions, e.Expression)
	}
	res.Class, res.Reasons = rules.ClassifyRule(expressions)
	if r.Enabled && res.Class == rules.NotApplicable {
		for _, reason := range res.Reasons {
			if !acceptedNotApplicable[reason] {
				res.Problems = append(res.Problems, "enabled rule is not applicable: "+strings.Join(res.Reasons, "; "))
				break
			}
		}
	}
	return res
}

func lint(src string, write bool) ([]result, string, error) {
	parsed, err := parseRules(src)
	if err != nil {
		return nil, "", err
	}
	lines := strings.Split(src, "\n")
	spans, err := ruleSpans(strings.Split(stripActions(src), "\n"))
	if err != nil {
		return nil, "", err
	}
	if len(spans) != len(parsed) {
		return nil, "", fmt.Errorf("found %d rule blocks in the text but %d rules in the document", len(spans), len(parsed))
	}
	results := make([]result, len(parsed))
	for i, r := range parsed {
		results[i] = classify(r)
	}
	if !write {
		for i, r := range parsed {
			if problem := checkTag(r.Tags, results[i].Class); problem != "" {
				results[i].Problems = append(results[i].Problems, problem)
			}
		}
		return results, src, nil
	}
	for i := len(spans) - 1; i >= 0; i-- {
		updated, _, err := setTag(lines, spans[i], results[i].Class)
		if err != nil {
			return nil, "", fmt.Errorf("rule %s: %w", results[i].ID, err)
		}
		lines = updated
	}
	return results, strings.Join(lines, "\n"), nil
}
