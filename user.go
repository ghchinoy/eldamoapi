package main

import (
	"time"
)

// User represents an authorized user in the 'authorized_users' Firestore collection.
type User struct {
	UID       string    `firestore:"uid"`
	Email     string    `firestore:"email"`
	Active    bool      `firestore:"active"`
	Roles     []string  `firestore:"roles"`
	Scopes    []string  `firestore:"scopes"`
	CreatedAt time.Time `firestore:"created_at"`
	UpdatedAt time.Time `firestore:"updated_at"`
}

// HasScope checks if the user has a specific required scope.
func (u *User) HasScope(required string) bool {
	for _, scope := range u.Scopes {
		if scope == required {
			return true
		}
	}
	return false
}
