package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstraintName(t *testing.T) {
	tests := map[string]struct{ msg, want string }{
		"email":            {"ORA-00001: unique constraint (AUTH_SVC.USERS_EMAIL_UQ) violated on table AUTH_SVC.USERS columns (EMAIL)", "USERS_EMAIL_UQ"},
		"tanpa schema":     {"ORA-00001: unique constraint (USERS_USERNAME_UQ) violated", "USERS_USERNAME_UQ"},
		"tanpa constraint": {"ORA-00942: table or view does not exist", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, constraintName(tt.msg))
		})
	}
}
