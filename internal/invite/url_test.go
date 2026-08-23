package invite

import "testing"

func TestNormalizeBase(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		wantErr  bool
	}{
		{"", "", false}, // unset is not an error: it means "not configured"
		{"  https://chronicle.example.com/  ", "https://chronicle.example.com", false},
		{"http://192.168.1.10:4009", "http://192.168.1.10:4009", false},
		{"https://chronicle.example.com///", "https://chronicle.example.com", false},
		{"chronicle.example.com", "", true},       // no scheme
		{"ftp://chronicle.example.com", "", true}, // wrong scheme
		{"https://", "", true}, // no host
	} {
		got, err := NormalizeBase(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeBase(%q) = %q, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeBase(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSignInURL(t *testing.T) {
	// Unset base yields no link at all, which leaves clients on their own
	// fallback rather than handing them one that scans to nothing.
	if got := SignInURL("", "chr_abc"); got != "" {
		t.Errorf("SignInURL with no base = %q, want empty", got)
	}

	got := SignInURL("https://chronicle.example.com", "chr_abc")
	if want := "https://chronicle.example.com/sign-in?token=chr_abc"; got != want {
		t.Errorf("SignInURL = %q, want %q", got, want)
	}

	// A token is base64url, but escaping is not optional: an unescaped '+' or
	// '&' would truncate the token at the receiving end.
	if got := SignInURL("https://x.example", "chr_a+b&c"); got != "https://x.example/sign-in?token=chr_a%2Bb%26c" {
		t.Errorf("SignInURL did not escape the token: %q", got)
	}
}
