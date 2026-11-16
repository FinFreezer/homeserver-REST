package functionality

import (
	"log"
	"net/http"

	"github.com/finfreezer/homeserver/internal/auth"
)

func CheckClearance(next http.Handler, conf *ApiConfig) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			token, err := GetSessionToken(r)
			if err != nil {
				log.Println(err)
				respondWithError(w, 403, "No active session found.", err)
				return
			}
			session, err := conf.Database.GetSession(r.Context(), auth.HashSessionToken(token))
			if err != nil {
				log.Println(err)
				respondWithError(w, 403, "No active session found.", err)
				return
			}
			user, err := conf.Database.FindUserByID(r.Context(), session.UserID)
			if err != nil {
				log.Println(err)
				respondWithError(w, 403, "No matching user found.", err)
				return
			}
			if VerifySession(user, session) && user.IsAdmin == 1 {
				log.Println("Served admin request.")
				next.ServeHTTP(w, r)
			}
			respondWithError(w, 403, "Missing permissions or invalid session.", err)
		})
}
