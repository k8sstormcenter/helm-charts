package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k8sstormcenter/sbob-go/rules"
)

const fixture = `{{- if and .Values.alertCRD.installDefault }}
apiVersion: kubescape.io/v1
kind: Rules
metadata:
  name: default-rules
  namespace: {{ .Values.ksNamespace }}
  labels:
    {{- include "labels" (dict
      "Chart" .Chart
      "Values" .Values) | nindent 4 }}
spec:
  rules:
    - description: "native rule with tags"
      enabled: true
      expressions:
        message: "'module ' + event.module"
        ruleExpression:
          - eventType: kmod
            expression: "event.module == 'evil'"
        uniqueId: "event.module"
      id: R1002
      name: "Kernel module load"
      tags:
        - kmod
        - "context:kubernetes"
    - description: "generated rule without tags"
      enabled: true
      expressions:
        message: "'open ' + event.path"
        ruleExpression:
          - eventType: open
            expression: "!cp.was_path_opened(event.containerId, event.path)"
        uniqueId: "event.path"
      id: R0002
      name: "File access"
    - description: "event only rule"
      enabled: true
      expressions:
        message: "'ptrace'"
        ruleExpression:
          - eventType: ptrace
            expression: "true"
        uniqueId: "event.comm"
      id: R1015
      name: "Ptrace"
      tags: []
{{- end }}
`

func TestStripActionsKeepsLinesAndParses(t *testing.T) {
	stripped := stripActions(fixture)
	if strings.Contains(stripped, "{{") || strings.Contains(stripped, "}}") {
		t.Fatalf("actions left in the text:\n%s", stripped)
	}
	if got, want := strings.Count(stripped, "\n"), strings.Count(fixture, "\n"); got != want {
		t.Fatalf("line count changed: got %d, want %d", got, want)
	}
	parsed, err := parseRules(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 3 {
		t.Fatalf("got %d rules, want 3", len(parsed))
	}
	if parsed[0].ID != "R1002" || !parsed[0].Enabled || len(parsed[0].Tags) != 2 {
		t.Fatalf("unexpected first rule: %+v", parsed[0])
	}
	if parsed[1].Expressions.RuleExpression[0].Expression != "!cp.was_path_opened(event.containerId, event.path)" {
		t.Fatalf("unexpected expression: %+v", parsed[1].Expressions)
	}
}

func TestVerifyReportsMissingTags(t *testing.T) {
	results, out, err := lint(fixture, false)
	if err != nil {
		t.Fatal(err)
	}
	if out != fixture {
		t.Fatal("verify mode changed the text")
	}
	want := []rules.Class{rules.Native, rules.Generated, rules.NotApplicable}
	for i, r := range results {
		if r.Class != want[i] {
			t.Errorf("rule %s: class %s, want %s", r.ID, r.Class, want[i])
		}
		if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "is missing") {
			t.Errorf("rule %s: problems %v, want one missing tag", r.ID, r.Problems)
		}
	}
}

func TestWriteInsertsTags(t *testing.T) {
	results, out, err := lint(fixture, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if len(r.Problems) != 0 {
			t.Errorf("rule %s: unexpected problems %v", r.ID, r.Problems)
		}
	}
	want := fixture
	want = strings.Replace(want, `        - "context:kubernetes"
`, `        - "context:kubernetes"
        - "portability:native"
`, 1)
	want = strings.Replace(want, `      name: "File access"
`, `      name: "File access"
      tags:
        - "portability:generated"
`, 1)
	want = strings.Replace(want, `      tags: []
`, `      tags:
        - "portability:not-applicable"
`, 1)
	if out != want {
		t.Fatalf("unexpected text after write:\n%s", out)
	}
	parsed, err := parseRules(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed[0].Tags; len(got) != 3 || got[2] != "portability:native" {
		t.Fatalf("first rule tags: %v", got)
	}
}

func TestWriteIsIdempotent(t *testing.T) {
	_, once, err := lint(fixture, true)
	if err != nil {
		t.Fatal(err)
	}
	results, twice, err := lint(once, true)
	if err != nil {
		t.Fatal(err)
	}
	if once != twice {
		t.Fatalf("second write changed the text:\n%s", twice)
	}
	verified, _, err := lint(once, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range results {
		if len(verified[i].Problems) != 0 {
			t.Errorf("rule %s: problems after write: %v", verified[i].ID, verified[i].Problems)
		}
	}
}

func TestStaleTagIsDetectedAndRewritten(t *testing.T) {
	_, tagged, err := lint(fixture, true)
	if err != nil {
		t.Fatal(err)
	}
	stale := strings.Replace(tagged, `"portability:generated"`, `'portability:native'`, 1)
	results, _, err := lint(stale, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results[1].Problems) != 1 || !strings.Contains(results[1].Problems[0], "portability:native is stale, want portability:generated") {
		t.Fatalf("problems: %v", results[1].Problems)
	}
	if len(results[0].Problems) != 0 || len(results[2].Problems) != 0 {
		t.Fatalf("unexpected problems on the other rules: %v %v", results[0].Problems, results[2].Problems)
	}
	_, fixed, err := lint(stale, true)
	if err != nil {
		t.Fatal(err)
	}
	if fixed != tagged {
		t.Fatalf("stale tag was not rewritten in place:\n%s", fixed)
	}
}

func TestDuplicateTagIsRejected(t *testing.T) {
	_, tagged, err := lint(fixture, true)
	if err != nil {
		t.Fatal(err)
	}
	doubled := strings.Replace(tagged, `        - "portability:native"
`, `        - "portability:native"
        - "portability:degraded"
`, 1)
	results, _, err := lint(doubled, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].Problems) != 1 || !strings.Contains(results[0].Problems[0], "more than one") {
		t.Fatalf("problems: %v", results[0].Problems)
	}
	if _, _, err := lint(doubled, true); err == nil {
		t.Fatal("write accepted two portability tags")
	}
}

func TestUnknownIDAndNotApplicable(t *testing.T) {
	src := `spec:
  rules:
    - id: R9999
      name: "unknown"
      enabled: true
      expressions:
        ruleExpression:
          - eventType: exec
            expression: "event.comm == 'sh'"
      tags:
        - "portability:native"
    - id: R0001
      name: "wrong arity"
      enabled: true
      expressions:
        ruleExpression:
          - eventType: exec
            expression: "!cp.was_executed(event.containerId)"
      tags:
        - "portability:not-applicable"
    - id: R0002
      name: "wrong arity but disabled"
      enabled: false
      expressions:
        ruleExpression:
          - eventType: open
            expression: "!cp.was_path_opened(event.path)"
      tags:
        - "portability:not-applicable"
    - id: R1016
      name: "no expressions"
      enabled: true
      expressions:
        ruleExpression: []
      tags:
        - "portability:not-applicable"
`
	results, _, err := lint(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].Problems) != 1 || results[0].Problems[0] != "id is not in the catalogue" {
		t.Errorf("unknown id: %v", results[0].Problems)
	}
	if len(results[1].Problems) != 1 || !strings.HasPrefix(results[1].Problems[0], "enabled rule is not applicable") {
		t.Errorf("enabled not applicable: %v", results[1].Problems)
	}
	if len(results[2].Problems) != 0 {
		t.Errorf("disabled not applicable: %v", results[2].Problems)
	}
	if results[3].Class != rules.NotApplicable || len(results[3].Problems) != 0 {
		t.Errorf("no expressions: %s %v", results[3].Class, results[3].Problems)
	}
}

func TestFlowStyleTagsAreRejectedOnWrite(t *testing.T) {
	src := `spec:
  rules:
    - id: R1002
      name: "flow"
      enabled: true
      expressions:
        ruleExpression:
          - eventType: kmod
            expression: "event.module == 'evil'"
      tags: [kmod]
`
	if _, _, err := lint(src, true); err == nil {
		t.Fatal("flow style tags were accepted")
	}
}

func TestRunWritesFileAndVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{path}, &stdout, &stderr); code != 1 {
		t.Fatalf("verify of an untagged file returned %d: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--write", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("write returned %d: %s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "R1002\tnative\tmodule-load\t") {
		t.Fatalf("unexpected report:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("verify after write returned %d: %s", code, stderr.String())
	}
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("missing argument returned %d", code)
	}
}
