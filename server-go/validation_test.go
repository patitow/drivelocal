package main

import (
	"testing"
)

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid path",
			path:    "folder/subfolder/file.txt",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: false, // Empty is allowed (root)
		},
		{
			name:    "path with path traversal",
			path:    "../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path with absolute path",
			path:    "/etc/passwd",
			wantErr: false, // validatePath doesn't check for absolute paths (handlers do)
		},
		{
			name:    "path with null byte",
			path:    "file\x00.txt",
			wantErr: true,
		},
		{
			name:    "path with backslash",
			path:    "folder\\subfolder",
			wantErr: false, // Backslashes are normalized
		},
		{
			name:    "reticências no nome da pasta",
			path:    "Animes/Espera... Volta/ep 01.mkv",
			wantErr: false,
		},
		{
			name:    "traversal no meio",
			path:    "Animes/../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "traversal com barra invertida",
			path:    "Animes\\..\\..\\segredo",
			wantErr: true,
		},
		{
			name:    "só ..",
			path:    "..",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		password string
		wantErr bool
	}{
		{
			name:     "valid password",
			password: "SecurePass123!",
			wantErr:  false,
		},
		{
			name:     "too short",
			password: "Short", // Less than 6 characters
			wantErr:  true,
		},
		{
			name:     "too long",
			password: func() string {
				s := make([]byte, 129)
				for i := range s {
					s[i] = 'a'
				}
				return string(s)
			}(),
			wantErr: true,
		},
		{
			name:     "empty password",
			password: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePassword() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name    string
		username string
		wantErr bool
	}{
		{
			name:     "valid username",
			username: "user123",
			wantErr:  false,
		},
		{
			name:     "username with spaces",
			username: "user name",
			wantErr:  true,
		},
		{
			name:     "empty username",
			username: "",
			wantErr:  true,
		},
		{
			name:     "too long username",
			username: func() string {
				s := make([]byte, 51)
				for i := range s {
					s[i] = 'a'
				}
				return string(s)
			}(),
			wantErr: true,
		},
		{
			name:     "username with special chars",
			username: "user@name",
			wantErr:  true, // @ is not allowed (only alphanumeric, underscore, hyphen)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUsername(tt.username)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUsername() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

