package functionality

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	cfg "github.com/finfreezer/homeserver/internal/config"
	"github.com/finfreezer/homeserver/internal/database"
	"github.com/joho/godotenv"
)

type ApiConfig struct {
	Database       *database.Queries
	Platform       string
	Logger         *slog.Logger
	Secret         string
	ApiKey         string
	Authorized     bool
	AssetRoot      string
	CurrentRoot    string
	ActiveSessions *cfg.Sessions
}

func OpenDatabase(programLogger *slog.Logger) (*ApiConfig, error) {
	var db *sql.DB
	var errLoop error
	programLogger.Debug("loading server data...",
		slog.String("component", "opendatabase"))
	godotenv.Load()
	platform := os.Getenv("PLATFORM")
	secret := os.Getenv("SECRET")
	if secret == "" {
		programLogger.Error("unset secret variable, please include an environment variable",
			slog.String("component", "opendatabase"))
	}
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		programLogger.Error("unset dbURL variable, please include an environment variable",
			slog.String("component", "opendatabase"))
	}
	ApiKey := os.Getenv("API_KEY")
	newRoot := os.Getenv("ASSET_ROOT")
	if newRoot == "" {
		programLogger.Error("unset root directory variable, please include root as an environment variable",
			slog.String("component", "opendatabase"))
	}
	for i := 0; i < 10; i++ {
		programLogger.Debug(fmt.Sprintf("calling sql.Open with %s", dbURL),
			slog.String("component", "opendatabase"))
		//db, errLoop = sql.Open("postgres", dbURL)
		db, errLoop = sql.Open("sqlite", dbURL)
		if errLoop != nil {
			programLogger.Error("sql.Open failure",
				slog.String("component", "opendatabase"),
				slog.String("error", fmt.Sprintf("%+v", errLoop)))
			time.Sleep(2 * time.Second)
			continue
		}
		errLoop = db.Ping()
		if errLoop == nil {
			break
		}

		db.Close()
		time.Sleep(2 * time.Second)
	}
	if errLoop != nil {
		programLogger.Error("error reaching database",
			slog.String("component", "opendatabase"),
			slog.String("error", fmt.Sprintf("%+v", errLoop)))
	}
	dbQueries := database.New(db)
	sessions := cfg.NewSessions()
	dbSessions, err := dbQueries.GetAllSessions(context.Background())
	if err != nil {
		programLogger.Error("error loading sessions",
			slog.String("component", "opendatabase"),
			slog.String("error", fmt.Sprintf("%+v", errLoop)))
	}
	expired := sessions.InitSessions(dbSessions)
	for _, session := range expired {
		dbQueries.DeleteSession(context.Background(), session)
	}

	newApiConf := ApiConfig{
		Database:       dbQueries,
		Platform:       platform,
		Logger:         programLogger,
		Secret:         secret,
		ApiKey:         ApiKey,
		Authorized:     false,
		AssetRoot:      newRoot,
		CurrentRoot:    newRoot,
		ActiveSessions: sessions,
	}
	return &newApiConf, nil
}
