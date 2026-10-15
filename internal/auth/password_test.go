package auth_test

import (
	"errors"
	"regexp"
	"testing"

	"github.com/saudm6/dfm-be/internal/auth"
	"github.com/saudm6/dfm-be/internal/domain"
)

func TestHashPasswordFormat(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	want := regexp.MustCompile(`^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)
	if !want.MatchString(hash) {
		t.Errorf("HashPassword() = %q, want match for %s", hash, want)
	}
	again, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if again == hash {
		t.Error("two hashes of one password are equal, want different salts")
	}
}

func TestVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	// Made by the reference C implementation through argon2-cffi 25.1.0:
	// low_level.hash_secret(b"password", b"somesaltsomesalt", time_cost=2,
	// memory_cost=65536, parallelism=4, hash_len=24, type=Type.ID).
	const reference = "$argon2id$v=19$m=65536,t=2,p=4$c29tZXNhbHRzb21lc2FsdA$PPDXCklQ9PKaL8a1fKNdKObJi/ZytpnC"
	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{"right password", "correct horse battery", hash, true},
		{"wrong password", "correct horse battery!", hash, false},
		{"empty password", "", hash, false},
		{"reference vector", "password", reference, true},
		{"reference vector wrong password", "Password", reference, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := auth.VerifyPassword(tt.password, tt.hash)
			if err != nil {
				t.Fatalf("VerifyPassword() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("VerifyPassword() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestVerifyPasswordMalformedHash(t *testing.T) {
	tests := []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=65536,t=2,p=4$c29tZXNhbHQ$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8",
		"$argon2id$v=16$m=65536,t=2,p=4$c29tZXNhbHQ$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8",
		"$argon2id$v=19$m=0,t=2,p=4$c29tZXNhbHQ$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8",
		"$argon2id$v=19$m=65536,t=2,p=4$!!$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8",
		"$argon2id$v=19$m=65536,t=2,p=4$c29tZXNhbHQ$",
	}
	for _, hash := range tests {
		ok, err := auth.VerifyPassword("password", hash)
		if err == nil || err.Error() != "auth: malformed argon2id hash" || ok {
			t.Errorf("VerifyPassword(%q) = %t, %v, want false, %q", hash, ok, err, "auth: malformed argon2id hash")
		}
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		in         string
		want       auth.Role
		marketData bool
		wantErr    string
	}{
		{in: "owner", want: auth.Owner, marketData: true},
		{in: "member", want: auth.Member, marketData: false},
		{in: "Owner", wantErr: `invalid role: unknown value "Owner"`},
		{in: "", wantErr: `invalid role: unknown value ""`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := auth.ParseRole(tt.in)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr || !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("ParseRole() error = %v, want %q wrapping domain.ErrInvalid", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("ParseRole() = %q, %v, want %q", got, err, tt.want)
			}
			if got.MarketData() != tt.marketData {
				t.Errorf("%s.MarketData() = %t, want %t", got, got.MarketData(), tt.marketData)
			}
		})
	}
}
