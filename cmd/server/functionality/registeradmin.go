package functionality

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/finfreezer/homeserver/internal/auth"
	"github.com/finfreezer/homeserver/internal/database"
	"github.com/google/uuid"
)

func AddAdmin(db *database.Queries, username, passwordhash, password string, logger *slog.Logger) bool {
	params := database.CreateUserParams{
		ID:           uuid.NewString(),
		Name:         username,
		PasswordHash: passwordhash,
		IsAdmin:      1,
	}
	logger.Debug("looking if user exists")
	user, err := db.FindUser(context.Background(), params.Name)
	if err == nil {
		match, err := auth.CheckPassword(password, user.PasswordHash)
		if match && err == nil {
			logger.Info(fmt.Sprintf("logged in as admin: %s", user.Name),
				slog.String("component", "addadmin"))
			addGuest(db, logger)
			return true
		}
		if !match || err != nil {
			logger.Error("error logging in as user, check password",
				slog.String("component", "addadmin"),
				slog.String("error", fmt.Sprintf("%+v", err)))
			return false
		}
	}

	logger.Info("no user found, creating admin user",
		slog.String("component", "addadmin"))
	user, err = db.CreateUser(context.Background(), params)
	if err != nil {
		logger.Error("unable to create priviliged user in the database",
			slog.String("component", "addadmin"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		return false
	}

	logger.Info(fmt.Sprintf("logged in as admin: %s", user.Name),
		slog.String("component", "addadmin"))
	addGuest(db, logger)
	addHacker(db, logger)
	return true
}

func addGuest(db *database.Queries, logger *slog.Logger) {
	guest, err := db.FindUser(context.Background(), "DevGuest")
	if guest.Name != "" && err == nil {
		return
	}
	guestPwHash, err := auth.CreatePasswordHash("GuestPass")
	if err != nil {
		logger.Error("unknown error hashing password",
			slog.String("component", "addguest"),
			slog.String("error", fmt.Sprintf("%+v", err)))
	}
	guestParams := database.CreateUserParams{
		ID:           uuid.NewString(),
		Name:         "DevGuest",
		PasswordHash: guestPwHash,
		IsAdmin:      1,
	}
	gstUsr, err := db.CreateUser(context.Background(), guestParams)
	if err != nil {
		logger.Error("unable to create priviliged guest in the database",
			slog.String("component", "addguest"),
			slog.String("error", fmt.Sprintf("%+v", err)))
	} else {
		logger.Info(fmt.Sprintf("added guest dev: %s\n", gstUsr.Name),
			slog.String("component", "addguest"))
	}
}

func addHacker(db *database.Queries, logger *slog.Logger) {
	guest, err := db.FindUser(context.Background(), "HackerMan")
	if guest.Name != "" && err == nil {
		return
	}
	guestPwHash, err := auth.CreatePasswordHash("pass1234")
	if err != nil {
		logger.Error("unknown error hashing password",
			slog.String("component", "addhacker"),
			slog.String("error", fmt.Sprintf("%+v", err)))
	}
	guestParams := database.CreateUserParams{
		ID:           uuid.NewString(),
		Name:         "HackerMan",
		PasswordHash: guestPwHash,
		IsAdmin:      0,
	}
	gstUsr, err := db.CreateUser(context.Background(), guestParams)
	if err != nil {
		logger.Error("unknown error adding hacker",
			slog.String("component", "addhacker"),
			slog.String("error", fmt.Sprintf("%+v", err)))
	} else {
		logger.Info(fmt.Sprintf("added hacker: %s\n", gstUsr.Name),
			slog.String("component", "addhacker"))
	}
}
