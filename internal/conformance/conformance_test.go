package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, dir, "account-owner.json", `{
	  "name": "account-owner-masked",
	  "pip": {"account": {"A123": {"ownerId": "u-1", "region": "US", "status": "active"}}},
	  "request": {
	    "method": "GET", "path": "/accounts/A123",
	    "headers": {"x-subject": "u-1", "x-claim-region": "US"}
	  },
	  "response": {"status": 200, "body": {
	    "accountNumber": "1234567890",
	    "holder": {"email": "jane.doe@example.com"},
	    "ssn": "111-22-3333"
	  }},
	  "expect": {
	    "allowed": true, "status": 200,
	    "masked": {
	      "accountNumber": "******7890",
	      "holder.email": "j***@example.com",
	      "ssn": "[REDACTED]"
	    }
	  }
	}`)
	writeFixture(t, dir, "account-denied.json", `{
	  "name": "account-other-region-denied",
	  "pip": {"account": {"A123": {"ownerId": "u-1", "region": "US", "status": "active"}}},
	  "request": {
	    "method": "GET", "path": "/accounts/A123",
	    "headers": {"x-subject": "u-2", "x-claim-region": "EU"}
	  },
	  "expect": {"allowed": false, "status": 403, "reason": "default:deny(no-allow-match)"}
	}`)
	return dir
}

func TestRunPasses(t *testing.T) {
	res, err := Run("../../integration/testdata/config", fixtures(t), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.Passed != 2 || res.Failed != 0 {
		t.Fatalf("result: %+v", res)
	}
}

func TestRunReportsFailure(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.json", `{
	  "name": "wrong-expectation",
	  "pip": {"account": {"A123": {"ownerId": "u-1", "region": "US", "status": "active"}}},
	  "request": {"method": "GET", "path": "/accounts/A123",
	              "headers": {"x-subject": "u-1", "x-claim-region": "US"}},
	  "expect": {"allowed": false}
	}`)
	res, err := Run("../../integration/testdata/config", dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 || len(res.Failures) != 1 {
		t.Fatalf("result: %+v", res)
	}
}
