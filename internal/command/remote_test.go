package command

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantUser string
		wantHost string
		wantPath string
		wantErr  bool
	}{
		{"standard", "root@192.168.1.1:repo.git", "root", "192.168.1.1", "repo.git", false},
		{"with path", "deploy@myserver.com:/opt/repos/project.git", "deploy", "myserver.com", "/opt/repos/project.git", false},
		{"no at sign", "192.168.1.1:repo.git", "", "", "", true},
		{"no colon", "root@192.168.1.1", "", "", "", true},
		{"empty string", "", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, host, path, err := ParseRemoteURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRemoteURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if user != tt.wantUser || host != tt.wantHost || path != tt.wantPath {
				t.Errorf("ParseRemoteURL(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.url, user, host, path, tt.wantUser, tt.wantHost, tt.wantPath)
			}
		})
	}
}

func TestIsHostedPlatform(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://github.com/user/repo.git", true},
		{"https://gitlab.com/user/repo.git", true},
		{"http://gitee.com/u/r.git", true},
		{"git@github.com:user/repo.git", true},
		{"git@gitlab.com:user/repo.git", true},
		// Self-hosted: unknown host, or custom IP/path.
		{"root@192.168.1.1:repo.git", false},
		{"deploy@myserver.com:/opt/repos/project.git", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsHostedPlatform(tt.url); got != tt.want {
			t.Errorf("IsHostedPlatform(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestValidateRemoteURL(t *testing.T) {
	valid := []string{
		"git@github.com:user/repo.git",
		"https://github.com/user/repo.git",
		"http://gitee.com/u/r.git",
		"root@192.168.1.1:repo.git",
		"deploy@myserver.com:/opt/repos/project.git",
		"  git@github.com:user/repo.git  ", // surrounding whitespace is trimmed
	}
	for _, url := range valid {
		if err := ValidateRemoteURL(url); err != nil {
			t.Errorf("ValidateRemoteURL(%q) = %v, want nil", url, err)
		}
	}

	invalid := []string{
		"not-a-url",                          // bare word — accepted before this check existed
		"",                                   // empty
		"https://github.com",                 // host only, no path
		"https://github.com/",                // empty path
		"ssh://git@github.com/user/repo.git", // valid git transport, unsupported shape here
		"/local/path/repo.git",               // local path
		"git@github.com",                     // missing :path
		"git@:repo.git",                      // empty host
		"git@github.com:",                    // empty path
		"git user@host:repo.git",             // embedded whitespace
	}
	for _, url := range invalid {
		if err := ValidateRemoteURL(url); err == nil {
			t.Errorf("ValidateRemoteURL(%q) = nil, want error", url)
		}
	}
}
