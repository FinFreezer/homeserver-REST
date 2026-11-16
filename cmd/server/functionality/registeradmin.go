package functionality

import (
	"context"
	"log"

	"github.com/finfreezer/homeserver/internal/auth"
	"github.com/finfreezer/homeserver/internal/database"
	"github.com/google/uuid"
)

func AddAdmin(db *database.Queries, username, passwordhash, password string) bool {
	params := database.CreateUserParams{
		ID:           uuid.NewString(),
		Name:         username,
		PasswordHash: passwordhash,
		IsAdmin:      1,
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
	addHacker(db)
	return true
}

func addGuest(db *database.Queries) {
	guest, err := db.FindUser(context.Background(), "DevGuest")
	if guest.Name != "" && err == nil {
		return
	}
	guestPwHash, err := auth.CreatePasswordHash("GuestPass")
	if err != nil {
		log.Println(err)
	}
	guestParams := database.CreateUserParams{
		ID:           uuid.NewString(),
		Name:         "DevGuest",
		PasswordHash: guestPwHash,
		IsAdmin:      1,
	}
	gstUsr, err := db.CreateUser(context.Background(), guestParams)
	if err != nil {
		log.Println("Failed creating guest dev.")
	} else {
		log.Printf("Added guest dev: %s\n", gstUsr.Name)
	}
}

func addHacker(db *database.Queries) {
	guest, err := db.FindUser(context.Background(), "HackerMan")
	if guest.Name != "" && err == nil {
		return
	}
	guestPwHash, err := auth.CreatePasswordHash("pass1234")
	if err != nil {
		log.Println(err)
	}
	guestParams := database.CreateUserParams{
		ID:           uuid.NewString(),
		Name:         "HackerMan",
		PasswordHash: guestPwHash,
		IsAdmin:      0,
	}
	gstUsr, err := db.CreateUser(context.Background(), guestParams)
	if err != nil {
		log.Println("Failed creating guest dev.")
	} else {
		log.Printf("Added hacker: %s\n", gstUsr.Name)
	}
}
