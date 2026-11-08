package repofile_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
	"github.com/byranZA/smith/internal/repofile"
)

func TestParseRefusesAnUnknownKeyNamingItsLine(t *testing.T) {
	t.Parallel()
	data := "push: true\nprepare: make\nmodle: opus\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 3, Message: `unknown field "modle"`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseReadsEveryField(t *testing.T) {
	t.Parallel()
	data := "presets:\n  codex: {agent: codex, model: gpt-5, effort: low}\nroles:\n  work: codex\npush: false\nprepare: make\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	off := false
	want := repofile.File{
		Presets: map[string]repofile.Preset{"codex": {Agent: "codex", Model: "gpt-5", Effort: "low"}},
		Roles:   map[string]string{"work": "codex"},
		Push:    &off,
		Prepare: "make",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseReadsTheLabelsBlock(t *testing.T) {
	t.Parallel()
	data := "labels:\n  agent: go\n  human: stop\n  spec: epic\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	want := repofile.File{Labels: repofile.Labels{Agent: "go", Human: "stop", Spec: "epic"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseRefusesAnUnknownLabelKeyNamingItsLine(t *testing.T) {
	t.Parallel()
	data := "push: true\nlabels:\n  agent: go\n  task: ready\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 4, Path: "labels", Message: `unknown field "task"`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesAnEmptyLabelNamingItsLine(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"quoted empty": "labels:\n  spec: epic\n  agent: \"\"\n",
		"no value":     "labels:\n  spec: epic\n  agent:\n",
		"null":         "labels:\n  spec: epic\n  agent: ~\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			want := []blueprint.Finding{{Line: 3, Path: "labels.agent", Message: "a label needs a name: set one, or leave the key out for the default"}}
			if !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}

func TestParseRefusesTwoLabelsSharingANameNamingTheLaterLine(t *testing.T) {
	t.Parallel()
	data := "labels:\n  agent: triaged\n  spec: epic\n  human: triaged\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 4, Path: "labels.human", Message: `label "triaged" is already labels.agent, on line 2: each label needs a name of its own`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesAnEmptyLabelGivenThroughAnAlias(t *testing.T) {
	t.Parallel()
	data := "presets:\n  plain: {agent: claude, model: &empty \"\"}\nlabels:\n  agent: *empty\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 4, Path: "labels.agent", Message: "a label needs a name: set one, or leave the key out for the default"}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseRefusesTwoLabelsSharingANameThroughAnAlias(t *testing.T) {
	t.Parallel()
	data := "labels:\n  agent: &shared triaged\n  human: *shared\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 3, Path: "labels.human", Message: `label "triaged" is already labels.agent, on line 2: each label needs a name of its own`}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseReadsALabelGivenThroughAnAlias(t *testing.T) {
	t.Parallel()
	data := "prepare: &name go\nlabels:\n  agent: *name\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	want := repofile.File{Prepare: "go", Labels: repofile.Labels{Agent: "go"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseReadsThePrepareCommand(t *testing.T) {
	t.Parallel()
	data := "push: true\nprepare: uv sync\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	on := true
	want := repofile.File{Push: &on, Prepare: "uv sync"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseRefusesAMalformedPrepareCommandNamingItsLine(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"list":         "push: true\nprepare:\n  - uv sync\n",
		"mapping":      "push: true\nprepare:\n  run: uv sync\n",
		"quoted empty": "push: true\nprepare: \"\"\n",
		"no value":     "push: true\nprepare:\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			want := []blueprint.Finding{{Line: 2, Path: "prepare", Message: "a prepare command is one non-empty string, such as \"uv sync\": set one, or leave the key out to run none"}}
			if !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}

func TestParseReadsAPrepareCommandSuppliedThroughAMerge(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"mapping":           "push: true\n<<: {prepare: uv sync}\n",
		"sequence":          "push: true\n<<: [{push: true}, {prepare: uv sync}]\n",
		"nested":            "push: true\n<<: {<<: {prepare: uv sync}}\n",
		"direct overrides":  "push: true\nprepare: uv sync\n<<: {prepare: []}\n",
		"first merge wins":  "push: true\n<<: [{prepare: uv sync}, {prepare: {}}]\n",
		"own key overrides": "push: true\n<<: {prepare: uv sync, <<: {prepare: \"\"}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := repofile.Parse([]byte(data))
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", data, err)
			}

			if got.Prepare != "uv sync" {
				t.Errorf("Parse(%q).Prepare = %q, want %q", data, got.Prepare, "uv sync")
			}
		})
	}
}

func TestParseRefusesAMalformedMergedPrepareCommandNamingItsLine(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"list":             "push: true\n<<: {prepare: []}\n",
		"mapping":          "push: true\n<<: {prepare: {}}\n",
		"quoted empty":     "push: true\n<<: {prepare: \"\"}\n",
		"sequence":         "push: true\n<<: [{push: true}, {prepare: []}]\n",
		"nested":           "push: true\n<<: {<<: {prepare: []}}\n",
		"first merge wins": "push: true\n<<: [{prepare: []}, {prepare: uv sync}]\n",
		"direct malformed": "push: true\nprepare: []\n<<: {prepare: uv sync}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			want := []blueprint.Finding{{Line: 2, Path: "prepare", Message: "a prepare command is one non-empty string, such as \"uv sync\": set one, or leave the key out to run none"}}
			if !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}

func TestParseRefusesAMalformedPrepareCommandOnTheLineOfItsMergedKey(t *testing.T) {
	t.Parallel()
	data := "push: true\n<<:\n  prepare: []\n"

	_, err := repofile.Parse([]byte(data))

	var verr *blueprint.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
	}
	want := []blueprint.Finding{{Line: 3, Path: "prepare", Message: "a prepare command is one non-empty string, such as \"uv sync\": set one, or leave the key out to run none"}}
	if !reflect.DeepEqual(verr.Findings, want) {
		t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
	}
}

func TestParseReadsTheStackBlock(t *testing.T) {
	t.Parallel()
	data := "stack:\n  up: docker compose up -d --wait\n  down: docker compose down\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	want := repofile.File{Stack: repofile.Stack{Up: "docker compose up -d --wait", Down: "docker compose down"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse(%q) = %+v, want %+v", data, got, want)
	}
}

func TestParseRefusesAnInvalidStackNamingItsLine(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		data string
		want blueprint.Finding
	}{
		"no down":       {"push: true\nstack:\n  up: make up\n", blueprint.Finding{Line: 2, Path: "stack", Message: "down is required: a stack must say how to stop what it starts"}},
		"no up":         {"push: true\nstack:\n  down: make down\n", blueprint.Finding{Line: 2, Path: "stack", Message: "up is required: a stack must say how to start"}},
		"empty up":      {"push: true\nstack:\n  up: \"\"\n  down: make down\n", blueprint.Finding{Line: 3, Path: "stack.up", Message: "up must be a non-empty string"}},
		"list down":     {"push: true\nstack:\n  up: make up\n  down:\n    - make down\n", blueprint.Finding{Line: 4, Path: "stack.down", Message: "down must be one string"}},
		"unknown key":   {"push: true\nstack:\n  up: make up\n  down: make down\n  wait: 5\n", blueprint.Finding{Line: 5, Path: "stack", Message: `unknown field "wait"`}},
		"not a mapping": {"push: true\nstack: make up\n", blueprint.Finding{Line: 2, Path: "stack", Message: "a stack is a mapping with an up and a down command"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(tc.data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", tc.data, err)
			}
			if want := []blueprint.Finding{tc.want}; !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", tc.data, verr.Findings, want)
			}
		})
	}
}

func TestParseLeavesTheStackEmptyWhenNoneIsDeclared(t *testing.T) {
	t.Parallel()

	got, err := repofile.Parse([]byte("push: true\n"))
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}

	if got.Stack != (repofile.Stack{}) {
		t.Errorf("Stack = %+v, want none", got.Stack)
	}
}

func TestParseReadsTheAgentRunsCap(t *testing.T) {
	t.Parallel()
	data := "limits:\n  agent_runs: 20\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	if got.Limits.AgentRuns != 20 {
		t.Errorf("Limits.AgentRuns = %d, want 20", got.Limits.AgentRuns)
	}
}

func TestParseTakesAnEmptyLimitsBlockAsNoCap(t *testing.T) {
	t.Parallel()
	for _, data := range []string{"limits:\n", "limits: {}\n", "push: true\n"} {
		got, err := repofile.Parse([]byte(data))
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", data, err)
		}
		if got.Limits.AgentRuns != 0 {
			t.Errorf("Parse(%q) Limits.AgentRuns = %d, want 0", data, got.Limits.AgentRuns)
		}
	}
}

func TestParseRefusesAnInvalidLimitsBlockNamingItsKey(t *testing.T) {
	t.Parallel()
	const whole = "agent_runs must be a whole number of at least 1"
	cases := map[string]struct {
		data string
		want blueprint.Finding
	}{
		"zero":          {"push: true\nlimits:\n  agent_runs: 0\n", blueprint.Finding{Line: 3, Path: "limits.agent_runs", Message: whole}},
		"negative":      {"push: true\nlimits:\n  agent_runs: -3\n", blueprint.Finding{Line: 3, Path: "limits.agent_runs", Message: whole}},
		"fraction":      {"push: true\nlimits:\n  agent_runs: 2.5\n", blueprint.Finding{Line: 3, Path: "limits.agent_runs", Message: whole}},
		"text":          {"push: true\nlimits:\n  agent_runs: many\n", blueprint.Finding{Line: 3, Path: "limits.agent_runs", Message: whole}},
		"empty":         {"push: true\nlimits:\n  agent_runs:\n", blueprint.Finding{Line: 3, Path: "limits.agent_runs", Message: whole}},
		"unknown key":   {"push: true\nlimits:\n  agent_run: 3\n", blueprint.Finding{Line: 3, Path: "limits", Message: `unknown field "agent_run"`}},
		"not a mapping": {"push: true\nlimits: 3\n", blueprint.Finding{Line: 2, Path: "limits", Message: "limits is a mapping, such as agent_runs: 20"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := repofile.Parse([]byte(tc.data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", tc.data, err)
			}
			if want := []blueprint.Finding{tc.want}; !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", tc.data, verr.Findings, want)
			}
		})
	}
}

func TestParseReadsTheTaskAttempts(t *testing.T) {
	t.Parallel()
	data := "limits:\n  task_attempts: 4\n"

	got, err := repofile.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", data, err)
	}

	if got.Limits.TaskAttempts != 4 {
		t.Errorf("Limits.TaskAttempts = %d, want 4", got.Limits.TaskAttempts)
	}
}

func TestParseRefusesInvalidTaskAttemptsNamingTheKey(t *testing.T) {
	t.Parallel()
	const whole = "task_attempts must be a whole number of at least 1"
	for name, value := range map[string]string{"zero": "0", "negative": "-1", "fraction": "1.5", "text": "twice", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := "push: true\nlimits:\n  task_attempts: " + value + "\n"

			_, err := repofile.Parse([]byte(data))

			var verr *blueprint.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse(%q) error = %v, want a *blueprint.ValidationError", data, err)
			}
			if want := []blueprint.Finding{{Line: 3, Path: "limits.task_attempts", Message: whole}}; !reflect.DeepEqual(verr.Findings, want) {
				t.Errorf("Parse(%q) findings = %+v, want %+v", data, verr.Findings, want)
			}
		})
	}
}
