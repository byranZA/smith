package tailscale

import "testing"

func TestParseAdminStatusRunningWithIdentity(t *testing.T) {
	data := []byte(`{
	  "BackendState": "Running",
	  "Self": { "UserID": 12345 },
	  "User": {
	    "12345": { "LoginName": "op@example.com" },
	    "999": { "LoginName": "someone@else.com" }
	  }
	}`)
	got, err := parseAdminStatus(data)
	if err != nil {
		t.Fatalf("parseAdminStatus() error = %v", err)
	}
	if !got.OnTailnet {
		t.Error("OnTailnet = false, want true for BackendState Running")
	}
	if got.Identity != "op@example.com" {
		t.Errorf("Identity = %q, want op@example.com resolved via the Self UserID", got.Identity)
	}
}

func TestParseAdminStatusStoppedIsOffTailnet(t *testing.T) {
	data := []byte(`{ "BackendState": "Stopped", "Self": { "UserID": 1 }, "User": {} }`)
	got, err := parseAdminStatus(data)
	if err != nil {
		t.Fatalf("parseAdminStatus() error = %v", err)
	}
	if got.OnTailnet {
		t.Error("OnTailnet = true, want false when BackendState is not Running")
	}
}
