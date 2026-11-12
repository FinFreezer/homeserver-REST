package sessions

import (
	"log"
	"time"

	"github.com/finfreezer/homeserver/internal/database"
)

type Sessions struct {
	Sessions []Session
}

type Session struct {
	UserID string
	Active bool
	Token  string
}

func NewSessions() *Sessions {
	return &Sessions{}
}

func (s *Sessions) InitSessions(dbSessions []database.Session) []string {
	expired := []string{}
	now := time.Now().UTC()
	for _, session := range dbSessions {
		expiry, err := time.Parse("02/01/2006 - 15:04", session.ExpiresAt)
		if err != nil {
			log.Println(err)
		}
		if expiry.Before(now) || session.RevokedAt.Valid {
			expired = append(expired, session.TokenHash)
			continue
		}
		newSession := Session{
			UserID: session.UserID,
			Active: true,
			Token:  session.TokenHash,
		}
		s.Sessions = append(s.Sessions, newSession)
	}
	return expired
}

func (s *Sessions) AddSession(user database.User) {
	session := Session{
		UserID: user.ID,
		Active: true,
		Token:  user.Authtoken.String,
	}
	s.Sessions = append(s.Sessions, session)
}
