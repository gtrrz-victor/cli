package cli

import (
	"testing"

	"github.com/entireio/cli/internal/coreapi"
)

func TestValidateGrantRole(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"reader", "writer", "admin"} {
		if err := validateGrantRole(ok); err != nil {
			t.Errorf("validateGrantRole(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "owner", "Reader", "member"} {
		if err := validateGrantRole(bad); err == nil {
			t.Errorf("validateGrantRole(%q) expected error", bad)
		}
	}
}

func TestParseOrgRole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    coreapi.AddOrgMemberInputBodyRole
		wantErr bool
	}{
		{in: "owner", want: coreapi.AddOrgMemberInputBodyRoleOwner},
		{in: "admin", want: coreapi.AddOrgMemberInputBodyRoleAdmin},
		{in: "member", want: coreapi.AddOrgMemberInputBodyRoleMember},
		{in: "", wantErr: true},
		{in: "viewer", wantErr: true},
		{in: "Owner", wantErr: true}, // case-sensitive: server enum is lowercase
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseOrgRole(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseOrgRole(%q) expected error, got %q", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOrgRole(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseOrgRole(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
