package audit

import "testing"

func TestRedactSensitive(t *testing.T) {
	in := map[string]string{
		"password": "a", "newPassword": "b", "X-Token": "c", "AUTHORIZATION": "d",
		"client_secret": "e", "SSN": "f", "status": "sent", "query.flowId": "lab",
	}
	out := RedactSensitive(in)
	for k, want := range map[string]string{
		"password": "[redacted]", "newPassword": "[redacted]", "X-Token": "[redacted]", "AUTHORIZATION": "[redacted]",
		"client_secret": "[redacted]", "SSN": "[redacted]", "status": "sent", "query.flowId": "lab",
	} {
		if out[k] != want {
			t.Errorf("%s = %q, want %q", k, out[k], want)
		}
	}
	if in["password"] != "a" {
		t.Error("RedactSensitive modified its input")
	}
}
