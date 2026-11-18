//go:build linux || darwin

package tracker

import (
	"os/user"
	"sync"
)

var userNames sync.Map // uid string -> name

// userName resolves a numeric uid to a login name.
func userName(uid string) string {
	if v, ok := userNames.Load(uid); ok {
		return v.(string)
	}
	name := uid
	if u, err := user.LookupId(uid); err == nil {
		name = u.Username
	}
	userNames.Store(uid, name)
	return name
}
