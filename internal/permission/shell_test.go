package permission

import "testing"

func TestClassifyShellAllow(t *testing.T) {
	for _, c := range []string{
		"go test ./...",
		"go build ./...",
		"git status",
		"git diff",
		"npm test",
		"cargo test",
		"ls -la",
		"echo hello",
	} {
		if got := ClassifyShell(c); got != Allow {
			t.Fatalf("%q => %v, want allow", c, got)
		}
	}
}

func TestClassifyShellDeny(t *testing.T) {
	for _, c := range []string{
		"rm -rf /",
		"rm -rf .",
		"git reset --hard HEAD",
		"git clean -fd",
		"mkfs.ext4 /dev/sda",
		"shutdown /s",
	} {
		if got := ClassifyShell(c); got != Deny {
			t.Fatalf("%q => %v, want deny", c, got)
		}
	}
}

func TestClassifyShellAsk(t *testing.T) {
	for _, c := range []string{
		"touch new.txt",
		"git commit -m x",
		"npm install",
	} {
		if got := ClassifyShell(c); got != Ask {
			t.Fatalf("%q => %v, want ask", c, got)
		}
	}
}

func TestClassifyShellDenyPathEscape(t *testing.T) {
	for _, c := range []string{
		"cat ../../etc/passwd",
		"cat ../secret",
		"type ..\\..\\windows\\win.ini",
		"cd ..",
		"cd .. && dir",
		"grep -r foo ../outside",
		"head ../secrets.txt",
		"ls ../..",
	} {
		if got := ClassifyShell(c); got != Deny {
			t.Fatalf("%q => %v, want deny", c, got)
		}
	}
}

func TestClassifyShellAbsolutePathRequiresAsk(t *testing.T) {
	for _, c := range []string{
		"cat /etc/passwd",
		"type C:\\Windows\\win.ini",
		"grep secret /home/user/.ssh/id_rsa",
	} {
		if got := ClassifyShell(c); got != Ask {
			t.Fatalf("%q => %v, want ask", c, got)
		}
	}
}

func TestClassifyShellAllowStillWorks(t *testing.T) {
	for _, c := range []string{
		"go test ./...",
		"cat README.md",
		"type README.md",
		"ls -la",
		"dir src",
		"grep TODO .",
	} {
		if got := ClassifyShell(c); got != Allow {
			t.Fatalf("%q => %v, want allow", c, got)
		}
	}
}

func TestShellAwarePolicyEscape(t *testing.T) {
	p := &ShellAwarePolicy{Inner: NewDefaultPolicy()}
	if p.Evaluate(Request{Tool: "shell", Arguments: `{"command":"cat ../../secret"}`}) != Deny {
		t.Fatal("shell path escape must deny")
	}
}

func TestShellAwarePolicy(t *testing.T) {
	p := &ShellAwarePolicy{Inner: NewDefaultPolicy()}

	if p.Evaluate(Request{Tool: "shell", Arguments: `{"command":"go test ./..."}`}) != Allow {
		t.Fatal("go test should allow")
	}
	if p.Evaluate(Request{Tool: "shell", Arguments: `{"command":"rm -rf ."}`}) != Deny {
		t.Fatal("rm -rf should deny")
	}
	if p.Evaluate(Request{Tool: "shell", Arguments: `{"command":"touch f"}`}) != Ask {
		t.Fatal("touch should ask")
	}
	if p.Evaluate(Request{Tool: "write_file"}) != Ask {
		t.Fatal("write_file still ask")
	}
	if p.Evaluate(Request{Tool: "read_file"}) != Allow {
		t.Fatal("read_file still allow")
	}
}
