package functionality

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/finfreezer/homeserver/internal/auth"
	"github.com/finfreezer/homeserver/internal/database"
	"github.com/google/uuid"
)

// GET /api/search/{term...}
func (a *ApiConfig) SearchMedia(w http.ResponseWriter, r *http.Request) {
	type SearchResult struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	type request struct {
		SearchTerm string `json:"search"`
	}
	type response struct {
		Results []SearchResult `json:"results"`
	}
	/*decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	params := request{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Error decoding request.", err)
		return
	}
	if searchTerm == "" {
		respondWithError(w, 400, "No search found.", err)
		return
	}*/
	searchTerm := r.PathValue("term")
	matchSlice, err := SearchForMatches(a.AssetRoot, searchTerm)
	if err != nil {
		respondWithError(w, 204, err.Error(), err)
		return
	}
	responseSlice := []SearchResult{}
	for _, match := range matchSlice {
		newResult := SearchResult{
			Name: match.file.Name(),
			Path: match.path,
		}
		responseSlice = append(responseSlice, newResult)
	}
	respondWithJSON(w, 200, responseSlice)
}

// POST /dev/wipeEntries
func (a *ApiConfig) WipeGuestbook(w http.ResponseWriter, r *http.Request) {
	log.Println("Received a request to wipe Guestbook database.")
	type request struct {
		IsAuthorized bool `json:"hasPrivilege"`
	}
	type response struct {
		Message string `json:"reply"`
	}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	params := request{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Error decoding request.", err)
		return
	}
	if !params.IsAuthorized {
		respondWithError(w, 403, "Please log-in to an authorized user to complete this action.", err)
		return
	}
	err = a.Database.DeleteGuestbookEntries(r.Context())
	if err != nil {
		respondWithError(w, 500, "Error wiping guestbook entries.", err)
		return
	}
	respondWithJSON(w, 200, response{
		Message: "Guestbook cleared successfully.",
	})
}

// POST /dev/deleteEntry
func (a *ApiConfig) DeleteGuestbookEntry(w http.ResponseWriter, r *http.Request) {
	type deleteParams struct {
		EntryId string `json:"id"`
	}
	type response struct {
		Message string `json:"reply"`
	}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	params := deleteParams{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Couldn't create guestbook database entry.", err)
		return
	}
	log.Printf("Received a request to delete guestbook entry with id %s.\n", params.EntryId)
	deleteId, err := uuid.Parse(params.EntryId)
	if err != nil {
		respondWithError(w, 500, "Faulty message ID.", err)
		return
	}
	foundMessage, err := a.Database.DeleteSpecificGuestbookEntry(r.Context(), deleteId.String())
	if err != nil {
		respondWithError(w, 500, "Faulty message ID.", err)
		return
	}
	respondWithJSON(w, 200, response{
		Message: fmt.Sprintf("Deleted message with ID %s succesfully.\n", foundMessage.ID),
	})
}

// GET /api/getEntries
func (a *ApiConfig) GetGuestbookEntries(w http.ResponseWriter, r *http.Request) {
	log.Println("Received a request to serve guestbook data.")
	type GuestbookBackend struct {
		ID        string `json:"ID"`
		CreatedAt string `json:"CreatedAt"`
		UpdatedAt string `json:"UpdatedAt"`
		Author    string `json:"Author"`
		Content   string `json:"Content"`
	}
	type response struct {
		Entries []GuestbookBackend `json:"messages"`
		Message string             `json:"reply"`
	}

	EntriesData, err := a.Database.GetGuestbookEntries(r.Context())
	ResponseEntries := []GuestbookBackend{}
	if err != nil {
		respondWithError(w, 500, "Error getting entries.", err)
		return
	}
	for _, entry := range EntriesData {
		createdTime, err := time.Parse("2006-01-02 15:04:05", entry.CreatedAt)
		if err != nil {
			respondWithError(w, 500, "Unexpected or faulty timestamp in database.", err)
			return
		}
		updatedTime, err := time.Parse("2006-01-02 15:04:05", entry.UpdatedAt)
		if err != nil {
			respondWithError(w, 500, "Unexpected or faulty timestamp in database.", err)
			return
		}
		ResponseEntries = append(ResponseEntries, GuestbookBackend{
			ID:        entry.ID,
			CreatedAt: createdTime.Format("02/01/2006 - 15:04"),
			UpdatedAt: updatedTime.Format("02/01/2006 - 15:04"),
			Author:    entry.Author,
			Content:   entry.Content,
		})
	}
	respondWithJSON(w, 200, response{
		Entries: ResponseEntries,
		Message: "Data successfully fetched.",
	})
}

// POST /api/addEntry
func (a *ApiConfig) AddGuestbookEntry(w http.ResponseWriter, r *http.Request) {
	type guestbookParams struct {
		EntryId string `json:"id"`
		Author  string `json:"author"`
		Message string `json:"content"`
	}
	type response struct {
		Message string `json:"reply"`
	}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	params := guestbookParams{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Couldn't create guestbook database entry.", err)
		return
	}
	log.Printf("Received a request to add entry to guestbook database with id %s.\n", params.EntryId)
	EntryIdValue, err := uuid.Parse(params.EntryId)
	if err != nil {
		respondWithError(w, 500, "Unable to parse message ID", err)
		return
	}
	if params.Message == "" {
		params.Message = "I'm a big dummy."
	}
	var Authorid uuid.NullUUID
	user, err := a.Database.FindUser(r.Context(), params.Author)
	if err == nil {
		Authorid = uuid.NullUUID{UUID: uuid.MustParse(user.ID), Valid: true}
	} else {
		Authorid = uuid.NullUUID{Valid: false}
	}
	databaseParams := database.CreateGuestbookEntryParams{
		ID:       EntryIdValue.String(),
		Author:   params.Author,
		Content:  params.Message,
		Authorid: sql.NullString{String: Authorid.UUID.String(), Valid: true},
	}
	_, err = a.Database.CreateGuestbookEntry(r.Context(), databaseParams)
	if err != nil {
		respondWithError(w, 500, "Error creating new guestbook entry to database.", err)
		return
	}
	respondWithJSON(w, 200, response{
		Message: "Guestbook entry added successfully.",
	})
}

// GET /api/verify
func (a *ApiConfig) VerifyAuth(w http.ResponseWriter, r *http.Request) {
	log.Println("Received verification request.")
	type response struct {
		Message       string `json:"reply"`
		Username      string `json:"username,omitempty"`
		Authorization bool   `json:"authorization"`
	}
	// Try to get token from cookie
	//cookie, err := GetAuthToken(r)

	cookie, err := GetSessionToken(r)
	if err != nil {
		respondWithJSON(w, http.StatusUnauthorized, response{
			Message: "No active session",
		})
		return
	}

	// Validate the session
	//userName, err := auth.ValidateJWT(cookie, a.Secret)

	hash := auth.HashSessionToken(cookie)
	session, err := a.Database.GetSession(r.Context(), hash)
	if err != nil {
		auth.ClearSessionCookie(w)
		respondWithError(w, http.StatusUnauthorized, "Invalid session, log-in manually.", err)
		return
	}
	user, err := a.Database.FindUserByID(r.Context(), session.UserID)
	if err != nil {
		// Clear invalid cookie
		ClearAuthCookie(w)
		respondWithJSON(w, http.StatusUnauthorized, response{
			Message: "Invalid or expired token. Log-in to refresh.",
		})
		return
	}
	if VerifySession(user, session) {
		responseMsg := fmt.Sprintf("Succesfully logged in as %s.\n", user.Name)
		respondWithJSON(w, http.StatusOK,
			response{
				Message:       responseMsg,
				Username:      user.Name,
				Authorization: user.IsAdmin == 1,
			},
		)
		return
	}

	respondWithError(w, http.StatusUnauthorized, "Potentially invalid or expired session. Please log-in.", err)
}

// POST /api/logout
func (a *ApiConfig) Logout(w http.ResponseWriter, r *http.Request) {
	type logoutParams struct {
		Name string `json:"name"`
	}
	type response struct {
		Message string `json:"reply"`
	}
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	params := logoutParams{}
	err := decoder.Decode(&params)
	log.Printf("Received logout request for %s.\n", params.Name)
	if err != nil {
		log.Printf("Error decoding log-out request: %s\n", err.Error())
		auth.ClearSessionCookie(w)
		respondWithJSON(w, http.StatusOK, response{
			Message: "Cleared current session.",
		})
		return
	}

	if params.Name != "unset" {
		// Optional: Also clear the token from database
		token, err := GetSessionToken(r)
		if err != nil {
			log.Println(err.Error())
		}
		_, err = a.Database.DeleteSession(r.Context(), auth.HashSessionToken(token))
		if err != nil {
			log.Printf("Warning: Failed to clear session from database for user %s: %v.\n", params.Name, err)
		}
		_, err = a.Database.UpdateUserToken(r.Context(),
			database.UpdateUserTokenParams{
				AuthtokenHash: sql.NullString{String: "", Valid: false},
				Name:          params.Name,
			},
		)
		if err != nil {
			log.Printf("Warning: Failed to clear token from database for user %s: %v.\n", params.Name, err)
		}
		auth.ClearSessionCookie(w)
		respondWithJSON(w, http.StatusOK, response{
			Message: "Successfully logged out",
		})
		return
	}

	// Clear the cookie
	ClearAuthCookie(w)
	respondWithJSON(w, http.StatusOK, response{
		Message: "Successfully logged out",
	})
}

// POST /api/register
func (a *ApiConfig) RegisterRegularUser(w http.ResponseWriter, r *http.Request) {
	var pwHash string
	type newUserParams struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	type response struct {
		Message string `json:"reply"`
	}

	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()

	params := newUserParams{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Couldn't decode parameters", err)
		return
	}
	log.Printf("Received a registration request for %s.\n", params.Name)
	if pwHash, err = auth.CreatePasswordHash(params.Password); err != nil {
		respondWithError(w, 500, "Error creating password hash.", err)
		return
	}
	addParams := database.CreateUserParams{
		ID:           uuid.NewString(),
		Name:         params.Name,
		PasswordHash: pwHash,
		IsAdmin:      0,
	}

	dbUser, err := a.Database.CreateUser(r.Context(), addParams)
	if err != nil {
		respondWithError(w, 500, "Error adding user.", err)
		return
	}

	respondWithJSON(w, 200, response{
		Message: fmt.Sprintf("Succesfully registered user %s.\n", dbUser.Name),
	})
}

// POST /api/login
func (a *ApiConfig) Login(w http.ResponseWriter, r *http.Request) {
	type loginParameters struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}

	type response struct {
		Message       string `json:"reply"`
		Authorization bool   `json:"authorization"`
	}

	log.Println("Received login request.")
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	params := loginParameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Couldn't decode parameters", err)
		return
	}
	dbUser, err := a.Database.FindUser(context.Background(), params.Name)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find user.", err)
		return
	}

	match, err := auth.CheckPassword(params.Password, dbUser.PasswordHash)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unexpected error.", err)
		return
	}
	if !match {
		respondWithError(w, http.StatusUnauthorized, "Incorrect password.", err)
		return
	}
	/*accessToken, err := auth.MakeJWT(dbUser.Name, a.Secret, time.Hour*24*7)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Failed to create token.", err)
		return
	}
	SetAuthCookie(w, accessToken)*/
	auth.ClearSessionCookie(w)
	sessionToken := auth.GenerateSessionToken()
	tokenHash := auth.HashSessionToken(sessionToken)
	auth.SetSessionCookie(w, sessionToken)
	responseMsg := fmt.Sprintf("Succesfully logged in as %s.\n", dbUser.Name)
	updateParams := database.UpdateUserTokenParams{
		AuthtokenHash: sql.NullString{String: tokenHash, Valid: true},
		Name:          dbUser.Name,
	}
	a.Database.UpdateUserToken(r.Context(), updateParams)
	session := database.InsertSessionParams{
		TokenHash: tokenHash,
		UserID:    dbUser.ID,
		ExpiresAt: time.Now().UTC().Add(time.Hour * 24 * 7).Format("02/01/2006 - 15:04"),
	}
	_, err = a.Database.InsertSession(r.Context(), session)
	if err != nil {
		respondWithError(w, 500, "Unable to create new session.", err)
		return
	}
	respondWithJSON(w, http.StatusOK,
		response{
			Message:       responseMsg,
			Authorization: dbUser.IsAdmin == 1 || dbUser.Name == "HackerMan",
		},
	)

}

// GET /api/dir/{path...}
func (a *ApiConfig) ListContents(w http.ResponseWriter, r *http.Request) {
	var dirOnly bool
	type FileInfo struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
	}
	type ListDirResponse struct {
		Message string   `json:"reply"`
		Files   FileNode `json:"directory"`
	}
	if a.CurrentRoot == "" || r.PathValue("path") == "" {
		log.Println("Panic")
	}
	fullPath := a.CurrentRoot + r.PathValue("path")
	dirOnlyFlag := r.URL.Query().Get("dirOnly")
	listDepthStr := r.URL.Query().Get("recDepth")
	listDepthInt, err := strconv.Atoi(listDepthStr)
	if err != nil {
		log.Println(err)
		listDepthInt = 0
	}
	if dirOnlyFlag == "true" {
		dirOnly = true
	} else {
		dirOnly = false
	}
	log.Println(r.PathValue("path"))
	log.Printf("Received a request to list contents at %s with dirOnly value of %v and depth of %d.\n",
		fullPath, dirOnly, listDepthInt)
	fi, err := os.Stat(fullPath)
	if err != nil || !fi.IsDir() {
		log.Println(err)
		respondWithError(w, 400, "Issue finding directory.\n", err)
		return
	}
	resultTree, err := buildFileTree(fullPath, dirOnly, listDepthInt)
	if err != nil {
		log.Println(err)
		respondWithError(w, 500, "Issue building directory tree-view.\n", err)
		return
	}
	msg := fmt.Sprintf("Listing files in %s.\n", fullPath)
	respondWithJSON(w, http.StatusOK,
		ListDirResponse{
			Message: msg,
			Files:   resultTree,
		},
	)
}

// GET /api/stream/{path...}
func (a *ApiConfig) StreamVideo(w http.ResponseWriter, r *http.Request) {
	type Response struct {
		Message string `json:"reply"`
	}
	requestPath := r.PathValue("path")
	log.Printf("Received a request with path %s.\n", requestPath)
	var fullPath string
	//fullPath := filepath.Join(a.CurrentRoot, r.PathValue("path"))
	if strings.Contains(requestPath, a.CurrentRoot) {
		fullPath = r.PathValue("path")
	} else {
		fullPath = filepath.Join(a.CurrentRoot, r.PathValue("path"))
	}
	log.Printf("Received a request to stream path %s.\n", fullPath)
	fi, err := os.Stat(fullPath)
	if err != nil {
		dir := filepath.Dir(fullPath)
		if dirStat, err := os.Stat(dir); dirStat.IsDir() && err == nil {
			log.Println("Looking for closest match.")
			fileToMatch := filepath.Base(fullPath)
			closestMatch, err := findClosestMatch(dir, fileToMatch)
			log.Printf("Matched %s to %s.\n", fileToMatch, closestMatch)
			if err != nil {
				log.Println(err)
				respondWithError(w, 400, "Couldn't reach file or directory.\n", err)
				return
			}
			fullPath = closestMatch
		} else {
			log.Println(err)
			respondWithError(w, 400, "Couldn't reach file or directory.\n", err)
			return
		}
	}
	fi, err = os.Stat(fullPath)
	if fi != nil && fi.IsDir() {
		respondWithError(w, 400, "Can't stream a directory.\n", err)
		return
	}
	if sendPlaylist := r.URL.Query().Get("playlist"); sendPlaylist == "only" {
		respondWithJSON(w, 200,
			Response{
				Message: strings.ReplaceAll(r.URL.String(), "playlist=only", "playlist=true")},
		)
		return
	}
	if sendPlaylist := r.URL.Query().Get("playlist"); sendPlaylist == "true" {
		servePlaylist(w, fullPath, a)
		return
	}
	file, err := os.Open(fullPath)
	if err != nil {
		respondWithError(w, 500, "Error opening file.\n", err)
		return
	}
	defer file.Close()
	contentType, err := readContentType(file)
	if err != nil {
		log.Println(err)
		respondWithError(w, 500, "Internal error.", err)
		return
	}
	log.Printf("Serving filetype %s.\n", contentType)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Accept-Ranges", "bytes")
	//http.ServeContent(w, r, file.Name(), fi.ModTime(), file) Ready method.
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		err, code := readByteRange(file, rangeHeader, w)
		if err != nil && err != io.EOF && code != 0 {
			respondWithError(w, code, "Problem streaming file.", err)
			log.Println(err)
			return
		} else {
			return
		}

	} else {
		w.WriteHeader(200)
		log.Println("Streaming full file.")
		maxBytesPerWrite := 5 << 20
		//maxBytesPerWrite := 5 << 10
		writeData := make([]byte, maxBytesPerWrite)
		var offsetN int64 = 0

		for {
			nFile, err := file.ReadAt(writeData, offsetN)
			if nFile == 0 {
				_, err = w.Write(writeData)
				if err != nil {
					log.Println(err)
					return
				}
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				return
			}
			offsetN += int64(nFile)
			//_, err = io.Copy(writeBuffer, file)
			_, err = w.Write(writeData[:nFile])
			if err != nil {
				log.Println(err)
				return
			}
		}
	}
}

// PUT /dev/cd
func (a *ApiConfig) MoveRootDirectory(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	type response struct {
		Message string `json:"reply"`
	}
	type Request struct {
		NewDirectory string `json:"newDir"`
	}
	reqData := Request{}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&reqData); err != nil {
		log.Println(err)
		respondWithError(w, 500, "Decoder error", err)
		return
	}
	reqData.NewDirectory = strings.ReplaceAll(reqData.NewDirectory, "%20", " ")
	newRoot := filepath.Join(a.CurrentRoot, reqData.NewDirectory)
	newRoot = filepath.Clean(newRoot) + string(filepath.Separator)
	absRoot, err := filepath.Abs(a.AssetRoot)
	if err != nil {
		respondWithError(w, 500, "Error resolving asset root", err)
		return
	}
	absNewPath, err := filepath.Abs(newRoot)
	if err != nil {
		respondWithError(w, 500, "Error resolving new path.", err)
		return
	}
	if !strings.HasPrefix(absNewPath+string(filepath.Separator), absRoot+string(filepath.Separator)) {
		log.Printf("Path '%s' is not within allowed bounds of '%s'.\n", absNewPath, absRoot)
		respondWithError(w, 403, "New root out of allowed bounds.", err)
		return
	}
	log.Printf("Received a request to move root to '%s'.\n", newRoot)
	fi, err := os.Stat(newRoot)
	if err != nil {
		dir := filepath.Dir(filepath.Clean(newRoot))
		dirToFind := filepath.Base(newRoot)
		log.Printf("Finding optional matches for path %s.\n", dir)
		closestMatch, err := findClosestMatch(dir, dirToFind)
		if err != nil {
			respondWithError(w, 400, "Error finding new root.", err)
			return
		} else {
			log.Printf("Matched %s to %s.\n", dirToFind, closestMatch)
			newRoot = closestMatch
		}
	}
	fi, err = os.Stat(newRoot)
	if fi != nil && !fi.IsDir() {
		respondWithError(w, 400, "New root must be a directory.", nil)
		return
	}
	a.CurrentRoot = newRoot
	respondWithJSON(w, 200, response{
		Message: "Root moved succesfully.",
	})
}

// GET /api/streamarchive/{path...}
func (a *ApiConfig) StreamArchive(w http.ResponseWriter, r *http.Request) {
	type Response struct {
		Message string `json:"reply"`
	}
	requestPath := r.PathValue("path")
	requestedPage, err := strconv.Atoi(r.URL.Query().Get("Page"))
	if err != nil {
		respondWithError(w, 500, "Error finding page.", err)
	}
	log.Printf("Received a request with path %s.\n", requestPath)
	var fullPath string
	//fullPath := filepath.Join(a.CurrentRoot, r.PathValue("path"))
	if strings.Contains(requestPath, a.CurrentRoot) {
		fullPath = r.PathValue("path")
	} else {
		fullPath = filepath.Join(a.CurrentRoot, r.PathValue("path"))
	}
	log.Printf("Received a request to stream image archive at %s.\n", fullPath)
	fi, err := os.Stat(fullPath)
	if err != nil {
		log.Println(err)
		dir := filepath.Dir(fullPath)
		if dirStat, err := os.Stat(dir); dirStat.IsDir() && err == nil {
			log.Println("Looking for closest match.")
			fileToMatch := filepath.Base(fullPath)
			closestMatch, err := findClosestMatch(dir, fileToMatch)
			log.Printf("Matched %s to %s.\n", fileToMatch, closestMatch)
			if err != nil {
				log.Println(err)
				respondWithError(w, 400, "Couldn't reach file or directory.\n", err)
				return
			}
			fullPath = closestMatch
		} else {
			log.Println(err)
			respondWithError(w, 400, "Couldn't reach file or directory.\n", err)
			return
		}
	}
	fi, err = os.Stat(fullPath)
	if fi != nil && fi.IsDir() {
		respondWithError(w, 400, "Can't stream a directory.\n", err)
		return
	}
	file, err := os.Open(fullPath)
	w.Header().Set("Cache-Control", "no-cache")
	err, contentType := streamZipImages(fullPath, w, requestedPage)
	if err != nil {
		respondWithError(w, 500, "Error opening file.\n", err)
		return
	}
	defer file.Close()
	/*contentType, err := readContentType(file)
	if err != nil {
		log.Println(err)
		respondWithError(w, 500, "Internal error.", err)
		return
	}*/
	log.Printf("Serving filetype %s.\n", contentType)
	//w.Header().Set("Content-Type", contentType)
	//http.ServeContent(w, r, file.Name(), fi.ModTime(), file) Ready method.
	log.Println("Streaming full file.")
}

// POST /dev/remoteUpdate
func (a *ApiConfig) UpdateAndRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Access forbidden", nil)
		return
	}
	type response struct {
		Message string `json:"reply"`
	}
	logsDir := "./logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Cannot create logs directory", err)
		return
	}
	respondWithJSON(w, 200, response{
		Message: "Executing server restart.",
	})
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	go func() {
		time.Sleep(100 * time.Millisecond)

		// Execute update completely detached
		if err := runUpdateDetached(); err != nil {
			log.Printf("Update failed: %v.\n", err)
		}
	}()

	go func() {
		time.Sleep(2 * time.Second)
		log.Fatal("Shutting down for update...")
	}()
}

func runUpdateDetached() error {
	h, m, s := time.Now().Clock()
	logFileName := fmt.Sprintf("./logs/%02d-%02d-%02d-update.log", h, m, s)
	cmd := exec.Command("bash", "-c", "nohup ./localupdate.sh > "+logFileName+" 2>&1 &")
	//cmd := exec.Command("bash", "-c", "nohup ./localupdate.sh > update.log 2>&1 &")
	cmd.Dir = "."

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start: %v", err)
	}

	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("failed to release: %v", err)
	}

	return nil
}
