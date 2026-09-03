package functionality

import (
	"context"
	"log"

	"github.com/finfreezer/homeserver/internal/auth"
	"github.com/finfreezer/homeserver/internal/database"
)

func AddAdmin(db *database.Queries, username, passwordhash, password string) bool {
	params := database.CreateUserParams{
		Name:         username,
		PasswordHash: passwordhash,
		IsAdmin:      true,
	}
	log.Println("Looking if user exists.")
	user, err := db.FindUser(context.Background(), params.Name)
	if err == nil {
		match, err := auth.CheckPassword(password, user.PasswordHash)
		if match && err == nil {
			log.Printf("Logged in as admin: %s", user.Name)
			addGuest(db)
			return true
		}
		if !match || err != nil {
			log.Println("Error logging in as user, check password.")
			return false
		}
	}

	log.Println("No user found, creating admin user.")
	user, err = db.CreateUser(context.Background(), params)
	if err != nil {
		log.Fatal(err)
		return false
	}

	log.Printf("Logged in as admin: %s\n", user.Name)
	addGuest(db)
	return true
}

func addGuest(db *database.Queries) {

	guestPwHash, err := auth.CreatePasswordHash("GuestPass")
	if err != nil {
		log.Println(err)
	}
	guestParams := database.CreateUserParams{
		Name:         "DevGuest",
		PasswordHash: guestPwHash,
		IsAdmin:      true,
	}
	gstUsr, err := db.CreateUser(context.Background(), guestParams)
	if err != nil {
		log.Println("Failed creating guest dev.")
	} else {
		log.Printf("Added guest dev: %s\n", gstUsr.Name)
	}
}
