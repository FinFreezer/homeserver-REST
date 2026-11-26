package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	pg "github.com/finfreezer/homeserver/cmd/server/functionality"
	"github.com/finfreezer/homeserver/internal/auth"
	"github.com/joho/godotenv"

	//_ "github.com/lib/pq"
	_ "github.com/glebarez/go-sqlite"
)

func main() {
	godotenv.Load()
	platform := os.Getenv("PLATFORM")
	var newLogHandler *slog.TextHandler
	if platform == "dev" {
		newLogHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	} else {
		newLogHandler = slog.NewTextHandler(os.Stderr, nil)
	}
	programLogger := slog.New(newLogHandler)
	user := os.Getenv("ADMIN_USER")
	programLogger.Info(fmt.Sprintf("Env User: %s", user))
	pass := os.Getenv("ADMIN_PASS")
	args := os.Args
	var newApiConf *pg.ApiConfig
	var err error
	if user == "" || pass == "" {
		if len(args) < 2 {
			fmt.Println("Please input login credentials.")
			os.Exit(1)
		}
		if args[1] == "delete" && os.Getenv("PLATFORM") == "dev" {
			newApiConf, err = pg.OpenDatabase(programLogger)
			if err != nil {
				programLogger.Error(fmt.Sprintf("error opening database: %+v", err),
					slog.String("component", "main"))
				os.Exit(1)
			}
			newApiConf.Database.DeleteUsers(context.Background())
			fmt.Println("Cleared 'users' table.")
			os.Exit(0)
		}
		if len(args) != 3 {
			fmt.Println("Please input the username and password you wish to use remotely.")
			os.Exit(1)
		}
		newApiConf, err = pg.OpenDatabase(programLogger)
		if err != nil {
			programLogger.Error(fmt.Sprintf("error opening database: %+v", err),
				slog.String("component", "main"))
			os.Exit(1)
		}
		pwHash, err := auth.CreatePasswordHash(args[2]) //Username + hashed password
		if err != nil {
			programLogger.Error(fmt.Sprintf("password hash creation failed: %+v", err),
				slog.String("component", "main"))
		}
		if ok := pg.AddAdmin(newApiConf.Database, args[1], pwHash, args[2], programLogger); !ok {
			programLogger.Error("error adding administrative account, exiting",
				slog.String("component", "main"))
			os.Exit(1)
		}
	} else {
		newApiConf, err = pg.OpenDatabase(programLogger)
		if err != nil {
			programLogger.Error(fmt.Sprintf("error opening database: %+v", err),
				slog.String("component", "main"))
			os.Exit(1)
		}
		pwHash, err := auth.CreatePasswordHash(pass) //Username + hashed password
		if err != nil {
			programLogger.Error(fmt.Sprintf("password hash creation failed: %+v", err),
				slog.String("component", "main"))
		}
		if ok := pg.AddAdmin(newApiConf.Database, user, pwHash, pass, programLogger); !ok {
			programLogger.Error("error adding administrative account, exiting",
				slog.String("component", "main"))
			os.Exit(1)
		}
	}
	outgoingPort := os.Getenv("DFLT_PORT")
	if outgoingPort == "" {
		outgoingPort = ":8080"
	}
	newMux := http.NewServeMux()
	backendorigin := os.Getenv("DST_SERVER") + os.Getenv("DFLT_PORT")
	newMux.HandleFunc("POST /api/login", newApiConf.Login)
	newMux.HandleFunc("GET /api/dir/{path...}", newApiConf.ListContents)
	newMux.HandleFunc("GET /api/stream/{path...}", newApiConf.StreamVideo)
	newMux.HandleFunc("GET /api/streamarchive/{path...}", newApiConf.StreamArchive)
	//newMux.HandleFunc("PUT /dev/cd", newApiConf.MoveRootDirectory)
	//newMux.HandleFunc("POST /dev/remoteUpdate", newApiConf.UpdateAndRestart)
	newMux.HandleFunc("POST /api/register", newApiConf.RegisterRegularUser)
	newMux.HandleFunc("GET /api/verify", newApiConf.VerifyAuth)
	newMux.HandleFunc("POST /api/logout", newApiConf.Logout)
	newMux.HandleFunc("POST /api/addEntry", newApiConf.AddGuestbookEntry)
	newMux.HandleFunc("GET /api/getEntries", newApiConf.GetGuestbookEntries)
	newMux.HandleFunc("GET /api/search/{term...}", newApiConf.SearchMedia)
	//newMux.HandleFunc("POST /dev/deleteEntry", newApiConf.DeleteGuestbookEntry)
	//newMux.HandleFunc("POST /dev/wipeEntries", newApiConf.WipeGuestbook)
	newMux.Handle("PUT /dev/cd", pg.CheckClearance(
		http.HandlerFunc(newApiConf.MoveRootDirectory), newApiConf,
	))
	newMux.Handle("POST /dev/remoteUpdate", pg.CheckClearance(
		http.HandlerFunc(newApiConf.MoveRootDirectory), newApiConf,
	))
	newMux.Handle("POST /dev/deleteEntry", pg.CheckClearance(
		http.HandlerFunc(newApiConf.DeleteGuestbookEntry), newApiConf,
	))
	newMux.Handle("POST /dev/wipeEntries", pg.CheckClearance(
		http.HandlerFunc(newApiConf.WipeGuestbook), newApiConf,
	))
	handler := pg.ApplyMiddleware(
		newMux,
		pg.IncludeSecureHeaders(backendorigin),
		pg.BlockCSRF(os.Getenv("WEBSITE_HOST"), programLogger),
		pg.RateLimitRequests(programLogger),
	)
	//newServer := http.Server{Addr: ":8080", Handler: newMux}
	//newServer := http.FileServer(http.Dir("./cmd/server"))
	newServer := http.FileServer(http.Dir("./assets"))
	newMux.Handle("/", newServer)
	//err = newServer.ListenAndServe()
	programLogger.Info(fmt.Sprintf("Listening and Serving on port %s", outgoingPort),
		slog.String("component", "main"))
	//err = http.ListenAndServe(outgoingPort, newMux)
	err = http.ListenAndServe(outgoingPort, handler)
	if err != nil {
		programLogger.Error(fmt.Sprintf("server shutting down with error: %+v", err),
			slog.String("component", "main"))
	}
}
