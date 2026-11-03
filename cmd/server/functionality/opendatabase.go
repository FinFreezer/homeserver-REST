package functionality

import (
	"database/sql"
	"log"
	"os"
	"time"

	"github.com/finfreezer/homeserver/internal/database"
	"github.com/joho/godotenv"
)

type ApiConfig struct {
	Database    *database.Queries
	Platform    string
	Secret      string
	ApiKey      string
	Authorized  bool
	AssetRoot   string
	CurrentRoot string
}

func OpenDatabase() (*ApiConfig, error) {
	var db *sql.DB
	var errLoop error
	log.Println("Loading server data...")
	godotenv.Load()
	platform := os.Getenv("PLATFORM")
	secret := os.Getenv("SECRET")
	if secret == "" {
		log.Println("Unset secret variable, please include an environment variable.")
	}
	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		log.Println("Unset dbURL variable, please include an environment variable.")
	}
	ApiKey := os.Getenv("API_KEY")
	newRoot := os.Getenv("ASSET_ROOT")
	if newRoot == "" {
		log.Println("Unset root directory, please include a root as an environment variable.")
	}
	for i := 0; i < 10; i++ {
		log.Printf("Calling sql.Open with %s", dbURL)
		//db, errLoop = sql.Open("postgres", dbURL)
		db, errLoop = sql.Open("sqlite", dbURL)
		if errLoop != nil {
			log.Printf("sql.Open failed: %v", errLoop)
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
		log.Println(errLoop)
	}
	dbQueries := database.New(db)
	newApiConf := ApiConfig{
		Database:    dbQueries,
		Platform:    platform,
		Secret:      secret,
		ApiKey:      ApiKey,
		Authorized:  false,
		AssetRoot:   newRoot,
		CurrentRoot: newRoot,
	}
	return &newApiConf, nil
}
