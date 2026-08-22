package cli

import (
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/inventory"
)

func TestSetupInheritsTheNameOnTheBoxsMarker(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@203.0.113.10"}}}`)

	_, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "public",
		names:      inventory.Naming{Marker: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"dev"`) {
		t.Errorf("inventory = %q, want the box still registered as dev", got)
	}
	if strings.Contains(got, `"203.0.113.10":`) {
		t.Errorf("inventory = %q, want no second entry named after the address the operator typed", got)
	}
}

func TestSetupUpdatesTheOneEntryWhenTheBoxsAddressChanged(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)

	_, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "tailscale", tailnetIP: "100.92.14.31",
		names: inventory.Naming{Marker: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"smith@100.92.14.31"`) {
		t.Errorf("inventory = %q, want dev mapped to the address smith just proved", got)
	}
	if strings.Contains(got, "100.92.14.7\"") {
		t.Errorf("inventory = %q, want the address it answered at before replaced", got)
	}
	if strings.Count(got, `"target"`) != 1 {
		t.Errorf("inventory = %q, want exactly one box", got)
	}
}

func TestSetupRenamesTheBoxWhenTheOperatorNamesItSomethingElse(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@203.0.113.10"}}}`)

	stdout, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "public",
		names:      inventory.Naming{Flag: "staging", Marker: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"staging"`) {
		t.Errorf("inventory = %q, want the box registered as staging", got)
	}
	if strings.Contains(got, `"dev"`) {
		t.Errorf("inventory = %q, want the entry moved rather than copied", got)
	}
	if !strings.Contains(stdout, "renamed") || !strings.Contains(stdout, "dev") {
		t.Errorf("stdout = %q, want the rename reported naming the old name", stdout)
	}
}

func TestSetupRegistersUnderTheBlueprintWhenNothingElseNamesTheBox(t *testing.T) {
	dir := t.TempDir()

	_, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "public",
		names:      inventory.Naming{Blueprint: "acme", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"acme"`) {
		t.Errorf("inventory = %q, want the box registered under the blueprint's name", got)
	}
}

func TestSetupRefusesANameAnotherBoxAlreadyHolds(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@203.0.113.10"}}}`)

	stdout, stderr, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "public",
		names:      inventory.Naming{Flag: "dev", Host: "198.51.100.7"},
	})

	if code == 0 {
		t.Fatal("exit code = 0, want a name held by a different box refused")
	}
	out := stdout + stderr
	if !strings.Contains(out, "--name") {
		t.Errorf("output = %q, want it to name the flag to pass instead", out)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"smith@203.0.113.10"`) {
		t.Errorf("inventory = %q, want the box that holds the name left alone", got)
	}
	if strings.Contains(got, "198.51.100.7") {
		t.Errorf("inventory = %q, want the refused box registered under nothing", got)
	}
}

func TestSetupNeverInventsASuffixedNameForACollidingBlueprintName(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"acme":{"target":"smith@203.0.113.10"}}}`)

	_, _, code := concludeAt(t, dir, &answeringSSH{}, setupConclusion{
		accessMode: "public",
		names:      inventory.Naming{Blueprint: "acme", Host: "198.51.100.7"},
	})

	if code == 0 {
		t.Fatal("exit code = 0, want two boxes from one blueprint refused rather than renamed")
	}
	if got := inventoryContent(t, dir); strings.Contains(got, "acme-2") {
		t.Errorf("inventory = %q, want no invented name", got)
	}
}

func TestSetupStoresTheOperatorsTargetVerbatimWithoutProbingIt(t *testing.T) {
	dir := t.TempDir()
	ssh := &answeringSSH{}

	_, stderr, code := concludeAt(t, dir, ssh, setupConclusion{
		accessMode: "public", target: "dev.internal",
		names: inventory.Naming{Flag: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(ssh.targets) != 0 {
		t.Errorf("ssh targets = %v, want a target the operator supplied never probed", ssh.targets)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"dev.internal"`) {
		t.Errorf("inventory = %q, want dev mapped to the target the operator supplied", got)
	}
	if strings.Contains(got, "smith@203.0.113.10") {
		t.Errorf("inventory = %q, want the derived address not stored alongside it", got)
	}
}

func TestSetupRegistersATargetTheOperatorSuppliedEvenWhenTheBoxDoesNotAnswer(t *testing.T) {
	dir := t.TempDir()

	_, stderr, code := concludeAt(t, dir, &refusingSSH{}, setupConclusion{
		accessMode: "public", target: "dev.internal",
		names: inventory.Naming{Flag: "dev", Host: "203.0.113.10"},
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0: an operator's own target is not smith's to verify (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"dev.internal"`) {
		t.Errorf("inventory = %q, want the target stored as the operator wrote it", got)
	}
}

func TestMachineAddRegistersUnderTheNameOnTheBoxsMarker(t *testing.T) {
	dir := t.TempDir()
	ssh := &provisionedSSH{markerJSON: `{"schema_version":2,"access_mode":"public","name":"dev"}`}

	stdout, stderr, code := runAdd(t, dir, ssh, "smith@100.92.14.7")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"dev"`) || !strings.Contains(got, `"smith@100.92.14.7"`) {
		t.Errorf("inventory = %q, want dev mapped to smith@100.92.14.7", got)
	}
	if strings.Contains(stdout, "no name") {
		t.Errorf("stdout = %q, want no fallback reported when the marker named the box", stdout)
	}
}

func TestMachineAddPrefersTheNameTheOperatorPassedOverTheMarkers(t *testing.T) {
	dir := t.TempDir()
	ssh := &provisionedSSH{markerJSON: `{"schema_version":2,"access_mode":"public","name":"dev"}`}

	_, stderr, code := runAdd(t, dir, ssh, "smith@100.92.14.7", "--name", "staging")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"staging"`) {
		t.Errorf("inventory = %q, want the box registered as staging", got)
	}
}

func TestMachineAddRefusesAMarkerNameHeldByADifferentBox(t *testing.T) {
	dir := t.TempDir()
	writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
	ssh := &provisionedSSH{markerJSON: `{"schema_version":2,"access_mode":"public","name":"dev"}`}

	stdout, stderr, code := runAdd(t, dir, ssh, "smith@198.51.100.7")

	if code == 0 {
		t.Fatal("exit code = 0, want a name held by a different box refused")
	}
	out := stdout + stderr
	if !strings.Contains(out, "already names a different box") {
		t.Errorf("output = %q, want it to say the name is taken", out)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"smith@100.92.14.7"`) || strings.Contains(got, "198.51.100.7") {
		t.Errorf("inventory = %q, want dev still mapped to the box that holds the name", got)
	}
}
