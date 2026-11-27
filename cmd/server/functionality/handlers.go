package functionality

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	type response struct {
		Results []SearchResult `json:"results"`
	}
	searchTerm := r.PathValue("term")
	matchSlice, err := SearchForMatches(a.AssetRoot, searchTerm, a.Logger)
	if err != nil {
		a.Logger.Error("responding with error",
			slog.String("error", fmt.Sprintf("%+v", err)))
		respondWithError(w, 204, err.Error(), err, a.Logger)
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
	a.Logger.Info("successfully found match(es)",
		slog.String("component", "searchmedia"))
	respondWithJSON(w, 200, responseSlice, a.Logger)
}

// POST /dev/wipeEntries
func (a *ApiConfig) WipeGuestbook(w http.ResponseWriter, r *http.Request) {
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
		respondWithError(w, 500, "Error decoding request.", err, a.Logger)
		return
	}
	if !params.IsAuthorized {
		respondWithError(w, 403, "Please log-in to an authorized user to complete this action.", err, a.Logger)
		return
	}
	err = a.Database.DeleteGuestbookEntries(r.Context())
	if err != nil {
		respondWithError(w, 500, "Error wiping guestbook entries.", err, a.Logger)
		return
	}
	a.Logger.Info("served successfull request to wipe guestbook")
	respondWithJSON(w, 200, response{
		Message: "Guestbook cleared successfully.",
	}, a.Logger)
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
		respondWithError(w, 500, "Couldn't create guestbook database entry.", err, a.Logger)
		return
	}
	deleteId, err := uuid.Parse(params.EntryId)
	if err != nil {
		respondWithError(w, 500, "Faulty message ID.", err, a.Logger)
		return
	}
	foundMessage, err := a.Database.DeleteSpecificGuestbookEntry(r.Context(), deleteId.String())
	if err != nil {
		respondWithError(w, 500, "Faulty message ID.", err, a.Logger)
		return
	}
	a.Logger.Info(fmt.Sprintf("succesfully served a request to delete guestbook entry with id %s", params.EntryId))
	respondWithJSON(w, 200, response{
		Message: fmt.Sprintf("Deleted message with ID %s succesfully.\n", foundMessage.ID),
	}, a.Logger)
}

// GET /api/getEntries
func (a *ApiConfig) GetGuestbookEntries(w http.ResponseWriter, r *http.Request) {
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
		respondWithError(w, 500, "Error getting entries.", err, a.Logger)
		return
	}
	for _, entry := range EntriesData {
		createdTime, err := time.Parse("2006-01-02 15:04:05", entry.CreatedAt)
		if err != nil {
			respondWithError(w, 500, "Unexpected or faulty timestamp in database.", err, a.Logger)
			return
		}
		updatedTime, err := time.Parse("2006-01-02 15:04:05", entry.UpdatedAt)
		if err != nil {
			respondWithError(w, 500, "Unexpected or faulty timestamp in database.", err, a.Logger)
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
	a.Logger.Info("succesfully served a request to get guestbook data")
	respondWithJSON(w, 200, response{
		Entries: ResponseEntries,
		Message: "Data successfully fetched.",
	}, a.Logger)
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
		respondWithError(w, 500, "Couldn't create guestbook database entry.", err, a.Logger)
		return
	}
	EntryIdValue, err := uuid.Parse(params.EntryId)
	if err != nil {
		respondWithError(w, 500, "Unable to parse message ID", err, a.Logger)
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
		respondWithError(w, 500, "Error creating new guestbook entry to database.", err, a.Logger)
		return
	}
	a.Logger.Info(fmt.Sprintf("succesfully served a request to create a new guestbook entry for ID %s", params.EntryId))
	respondWithJSON(w, 200, response{
		Message: "Guestbook entry added successfully.",
	}, a.Logger)
}

// GET /api/verify
func (a *ApiConfig) VerifyAuth(w http.ResponseWriter, r *http.Request) {
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
		}, a.Logger)
		return
	}

	// Validate the session
	//userName, err := auth.ValidateJWT(cookie, a.Secret)

	hash := auth.HashSessionToken(cookie)
	session, err := a.Database.GetSession(r.Context(), hash)
	if err != nil {
		auth.ClearSessionCookie(w)
		respondWithError(w, http.StatusUnauthorized, "Invalid session, log-in manually.", err, a.Logger)
		return
	}
	user, err := a.Database.FindUserByID(r.Context(), session.UserID)
	if err != nil {
		// Clear invalid cookie
		ClearAuthCookie(w)
		respondWithJSON(w, http.StatusUnauthorized, response{
			Message: "Invalid or expired token. Log-in to refresh.",
		}, a.Logger)
		return
	}
	if VerifySession(user, session, a.Logger) {
		a.Logger.Info(fmt.Sprintf("succesfully server a verification request for user %s", user.Name))
		responseMsg := fmt.Sprintf("Succesfully logged in as %s.\n", user.Name)
		respondWithJSON(w, http.StatusOK,
			response{
				Message:       responseMsg,
				Username:      user.Name,
				Authorization: user.IsAdmin == 1,
			}, a.Logger,
		)
		return
	}

	respondWithError(w, http.StatusUnauthorized, "Potentially invalid or expired session. Please log-in.", err, a.Logger)
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
	if err != nil {
		a.Logger.Error("error decoding log-out request",
			slog.String("component", "logout"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		auth.ClearSessionCookie(w)
		respondWithJSON(w, http.StatusOK, response{
			Message: "Cleared current session.",
		}, a.Logger)
		return
	}

	if params.Name != "unset" {
		// Optional: Also clear the token from database
		token, err := GetSessionToken(r)
		if err != nil {
			a.Logger.Error("error reading session token",
				slog.String("component", "logout"),
				slog.String("error", fmt.Sprintf("%+v", err)))
		}
		_, err = a.Database.DeleteSession(r.Context(), auth.HashSessionToken(token))
		if err != nil {
			a.Logger.Error(fmt.Sprintf("failed to clear session from database for user %s", params.Name),
				slog.String("component", "logout"),
				slog.String("error", fmt.Sprintf("%+v", err)))
		}
		_, err = a.Database.UpdateUserToken(r.Context(),
			database.UpdateUserTokenParams{
				AuthtokenHash: sql.NullString{String: "", Valid: false},
				Name:          params.Name,
			},
		)
		if err != nil {
			a.Logger.Error(fmt.Sprintf("failed to clear token from database for user %s", params.Name),
				slog.String("component", "logout"),
				slog.String("error", fmt.Sprintf("%+v", err)))
		}
		auth.ClearSessionCookie(w)
		respondWithJSON(w, http.StatusOK, response{
			Message: "Successfully logged out",
		}, a.Logger)
		return
	}

	// Clear the cookie
	ClearAuthCookie(w)
	respondWithJSON(w, http.StatusOK, response{
		Message: "Successfully logged out",
	}, a.Logger)
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
		respondWithError(w, 500, "Couldn't decode parameters", err, a.Logger)
		return
	}
	if pwHash, err = auth.CreatePasswordHash(params.Password); err != nil {
		respondWithError(w, 500, "Error creating password hash.", err, a.Logger)
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
		respondWithError(w, 500, "Error adding user.", err, a.Logger)
		return
	}

	a.Logger.Info(fmt.Sprintf("server a successful registration request for new user %s", dbUser.Name))
	respondWithJSON(w, 200, response{
		Message: fmt.Sprintf("Succesfully registered user %s.\n", dbUser.Name),
	}, a.Logger)
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

	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	params := loginParameters{}
	err := decoder.Decode(&params)
	if err != nil {
		respondWithError(w, 500, "Couldn't decode parameters", err, a.Logger)
		return
	}
	dbUser, err := a.Database.FindUser(context.Background(), params.Name)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find user.", err, a.Logger)
		return
	}

	match, err := auth.CheckPassword(params.Password, dbUser.PasswordHash)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unexpected error.", err, a.Logger)
		return
	}
	if !match {
		respondWithError(w, http.StatusUnauthorized, "Incorrect password.", err, a.Logger)
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
	_, updateErr := a.Database.UpdateUserToken(r.Context(), updateParams)
	if updateErr != nil {
		a.Logger.Error("error updating user session token",
			slog.String("component", "login"),
			slog.String("error", fmt.Sprintf("%+v", updateErr)))
	}
	session := database.InsertSessionParams{
		TokenHash: tokenHash,
		UserID:    dbUser.ID,
		ExpiresAt: time.Now().UTC().Add(time.Hour * 24 * 7).Format("02/01/2006 - 15:04"),
	}
	_, err = a.Database.InsertSession(r.Context(), session)
	if err != nil {
		respondWithError(w, 500, "Unable to create new session.", err, a.Logger)
		return
	}
	a.Logger.Info(fmt.Sprintf("served successful login request for use %s", dbUser.Name))
	respondWithJSON(w, http.StatusOK,
		response{
			Message:       responseMsg,
			Authorization: dbUser.IsAdmin == 1 || dbUser.Name == "HackerMan",
		}, a.Logger,
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
	fullPath := a.CurrentRoot + r.PathValue("path")
	dirOnlyFlag := r.URL.Query().Get("dirOnly")
	listDepthStr := r.URL.Query().Get("recDepth")
	listDepthInt, err := strconv.Atoi(listDepthStr)
	if err != nil {
		a.Logger.Error("invalid conversion from string to integer",
			slog.String("component", "listcontents"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		listDepthInt = 0
	}
	if dirOnlyFlag == "true" {
		dirOnly = true
	} else {
		dirOnly = false
	}
	a.Logger.Debug(fmt.Sprintf("received a request to list contents at %s + %s with dirOnly value of %v and depth of %d",
		a.CurrentRoot, r.PathValue("path"), dirOnly, listDepthInt))
	//fi, err := os.Stat(fullPath)
	fi, err := a.RootFileHandler.Stat(fullPath)
	if err != nil || !fi.IsDir() {
		a.Logger.Error("unable to read directory",
			slog.String("component", "listcontents"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		respondWithError(w, 400, "Issue finding directory.\n", err, a.Logger)
		return
	}
	resultTree, err := buildFileTree(fullPath, dirOnly, listDepthInt, a.Logger, a.RootFileHandler)
	if err != nil {
		a.Logger.Error("failed to build internal file tree",
			slog.String("component", "listcontents"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		respondWithError(w, 500, "Issue building directory tree-view.\n", err, a.Logger)
		return
	}
	a.Logger.Info(fmt.Sprintf("successfully served a request to list files in %s", fullPath),
		slog.String("component", "listcontents"))
	msg := fmt.Sprintf("Listing files in %s.\n", fullPath)
	respondWithJSON(w, http.StatusOK,
		ListDirResponse{
			Message: msg,
			Files:   resultTree,
		}, a.Logger,
	)
}

// GET /api/stream/{path...}
func (a *ApiConfig) StreamVideo(w http.ResponseWriter, r *http.Request) {
	type Response struct {
		Message string `json:"reply"`
	}
	requestPath := r.PathValue("path")
	var fullPath string
	//fullPath := filepath.Join(a.CurrentRoot, r.PathValue("path"))
	if strings.Contains(requestPath, a.CurrentRoot) {
		fullPath = r.PathValue("path")
	} else {
		fullPath = filepath.Join(a.CurrentRoot, r.PathValue("path"))
	}
	a.Logger.Debug(fmt.Sprintf("received a request to stream path %s", fullPath),
		slog.String("component", "streamvideo"))
	//fi, err := os.Stat(fullPath)
	fi, err := a.RootFileHandler.Stat(fullPath)
	if err != nil {
		dir := filepath.Dir(fullPath)
		//dirStat, err := os.Stat(dir)
		if dirStat, err := a.RootFileHandler.Stat(dir); dirStat.IsDir() && err == nil {
			a.Logger.Debug("looking for closest match",
				slog.String("component", "streamvideo"))
			fileToMatch := filepath.Base(fullPath)
			closestMatch, err := findClosestMatch(dir, fileToMatch)
			a.Logger.Debug(fmt.Sprintf("matched %s to %s", fileToMatch, closestMatch))
			if err != nil {
				a.Logger.Error("error finding match",
					slog.String("component", "streamvideo"),
					slog.String("error", fmt.Sprintf("%+v", err)))
				respondWithError(w, 400, "Couldn't reach file or directory.\n", err, a.Logger)
				return
			}
			fullPath = closestMatch
		} else {
			a.Logger.Error("error reading path or invalid target",
				slog.String("component", "streamvideo"),
				slog.String("error", fmt.Sprintf("%+v", err)))
			respondWithError(w, 400, "Couldn't reach file or directory.\n", err, a.Logger)
			return
		}
	}
	//fi, err = os.Stat(fullPath)
	fi, err = a.RootFileHandler.Stat(fullPath)
	if fi != nil && fi.IsDir() {
		respondWithError(w, 400, "Can't stream a directory.\n", err, a.Logger)
		return
	}
	if sendPlaylist := r.URL.Query().Get("playlist"); sendPlaylist == "only" {
		respondWithJSON(w, 200,
			Response{
				Message: strings.ReplaceAll(r.URL.String(), "playlist=only", "playlist=true")}, a.Logger,
		)
		return
	}
	if sendPlaylist := r.URL.Query().Get("playlist"); sendPlaylist == "true" {
		servePlaylist(w, fullPath, a)
		return
	}
	//file, err := os.Open(fullPath)
	file, err := a.RootFileHandler.Open(fullPath)
	if err != nil {
		respondWithError(w, 500, "Error opening file.\n", err, a.Logger)
		return
	}
	defer file.Close()
	contentType, err := readContentType(file)
	if err != nil {
		a.Logger.Error("error determining content type",
			slog.String("component", "streamvideo"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		respondWithError(w, 500, "Internal error.", err, a.Logger)
		return
	}
	a.Logger.Debug(fmt.Sprintf("serving filetype %s", contentType),
		slog.String("component", "streamvideo"))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Accept-Ranges", "bytes")
	//http.ServeContent(w, r, file.Name(), fi.ModTime(), file) Ready method.
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		err, code := readByteRange(file, rangeHeader, w, a.Logger)
		if err != nil && err != io.EOF && code != 0 {
			respondWithError(w, code, "Problem streaming file.", err, a.Logger)
			a.Logger.Error("error reading file byterange",
				slog.String("component", "streamvideo"),
				slog.String("error", fmt.Sprintf("%+v", err)))
			return
		} else {
			return
		}

	} else {
		w.WriteHeader(200)
		a.Logger.Info("successfully serving full-file streaming request")
		maxBytesPerWrite := 5 << 20
		//maxBytesPerWrite := 5 << 10
		writeData := make([]byte, maxBytesPerWrite)
		var offsetN int64 = 0

		for {
			nFile, err := file.ReadAt(writeData, offsetN)
			if nFile == 0 {
				// #nosec G705 Writes only trusted data from backend
				_, err = w.Write(writeData)
				if err != nil {
					a.Logger.Error("error writing data to response",
						slog.String("component", "streamvideo"),
						slog.String("error", fmt.Sprintf("%+v", err)))
					return
				}
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				return
			}
			offsetN += int64(nFile)
			//_, err = io.Copy(writeBuffer, file)
			// #nosec G705 Writes only trusted data from backend
			_, err = w.Write(writeData[:nFile])
			if err != nil {
				a.Logger.Error("error writing data to response",
					slog.String("component", "streamvideo"),
					slog.String("error", fmt.Sprintf("%+v", err)))
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
		a.Logger.Error("error decoding request",
			slog.String("component", "moverootdirectory"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		respondWithError(w, 500, "Decoder error", err, a.Logger)
		return
	}
	reqData.NewDirectory = strings.ReplaceAll(reqData.NewDirectory, "%20", " ")
	newRoot := filepath.Join(a.CurrentRoot, reqData.NewDirectory)
	newRoot = filepath.Clean(newRoot) + string(filepath.Separator)
	absRoot, err := filepath.Abs(a.AssetRoot)
	if err != nil {
		respondWithError(w, 500, "Error resolving asset root", err, a.Logger)
		return
	}
	absNewPath, err := filepath.Abs(newRoot)
	if err != nil {
		respondWithError(w, 500, "Error resolving new path.", err, a.Logger)
		return
	}
	if !strings.HasPrefix(absNewPath+string(filepath.Separator), absRoot+string(filepath.Separator)) {
		a.Logger.Error(fmt.Sprintf("trying to reach out of bounds path, '%s' is not within allowed bounds of '%s'", absNewPath, absRoot),
			slog.String("component", "moverootdirectory"))
		respondWithError(w, 403, "New root out of allowed bounds.", err, a.Logger)
		return
	}
	a.Logger.Debug(fmt.Sprintf("received a request to move root to '%s'", newRoot),
		slog.String("component", "moverootdirectory"))
	//fi, err := os.Stat(newRoot)
	fi, err := a.RootFileHandler.Stat(newRoot)
	if err != nil {
		dir := filepath.Dir(filepath.Clean(newRoot))
		dirToFind := filepath.Base(newRoot)
		a.Logger.Debug(fmt.Sprintf("finding optional matches for path %s.\n", dir),
			slog.String("component", "moverootdirectory"))
		closestMatch, err := findClosestMatch(dir, dirToFind)
		if err != nil {
			respondWithError(w, 400, "Error finding new root.", err, a.Logger)
			return
		} else {
			a.Logger.Debug(fmt.Sprintf("matched %s to %s.\n", dirToFind, closestMatch),
				slog.String("component", "moverootdirectory"))
			newRoot = closestMatch
		}
	}
	//fi, err = os.Stat(newRoot)
	fi, err = a.RootFileHandler.Stat(newRoot)
	if fi != nil && !fi.IsDir() {
		respondWithError(w, 400, "New root must be a directory.", nil, a.Logger)
		return
	}
	a.CurrentRoot = newRoot
	a.Logger.Info(fmt.Sprintf("successfully served a request to move root to %s", newRoot))
	respondWithJSON(w, 200, response{
		Message: "Root moved successfully.",
	}, a.Logger)
}

// GET /api/streamarchive/{path...}
func (a *ApiConfig) StreamArchive(w http.ResponseWriter, r *http.Request) {
	type Response struct {
		Message string `json:"reply"`
	}
	requestPath := r.PathValue("path")
	requestedPage, err := strconv.Atoi(r.URL.Query().Get("Page"))
	if err != nil {
		respondWithError(w, 500, "Error finding page.", err, a.Logger)
	}
	var fullPath string
	//fullPath := filepath.Join(a.CurrentRoot, r.PathValue("path"))
	if strings.Contains(requestPath, a.CurrentRoot) {
		fullPath = r.PathValue("path")
	} else {
		fullPath = filepath.Join(a.CurrentRoot, r.PathValue("path"))
	}
	a.Logger.Debug(fmt.Sprintf("received a request to stream image archive at %s.\n", fullPath),
		slog.String("component", "streamarchive"))
	//fi, err := os.Stat(fullPath)
	fi, err := a.RootFileHandler.Stat(fullPath)
	if err != nil {
		a.Logger.Error("error reading file",
			slog.String("component", "streamarchive"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		dir := filepath.Dir(fullPath)
		//dirStat, err := os.Stat(dir)
		if dirStat, err := a.RootFileHandler.Stat(dir); dirStat.IsDir() && err == nil {
			a.Logger.Debug("looking for closest match",
				slog.String("component", "streamarchive"))
			fileToMatch := filepath.Base(fullPath)
			closestMatch, err := findClosestMatch(dir, fileToMatch)
			a.Logger.Debug(fmt.Sprintf("matched %s to %s.\n", fileToMatch, closestMatch),
				slog.String("component", "streamarchive"))
			if err != nil {
				a.Logger.Error("error finding matching files",
					slog.String("component", "streamarchive"),
					slog.String("error", fmt.Sprintf("%+v", err)))
				respondWithError(w, 400, "Couldn't reach file or directory.\n", err, a.Logger)
				return
			}
			fullPath = closestMatch
		} else {
			a.Logger.Error("error reading file or invalid path",
				slog.String("component", "streamarchive"),
				slog.String("error", fmt.Sprintf("%+v", err)))
			respondWithError(w, 400, "Couldn't reach file or directory.\n", err, a.Logger)
			return
		}
	}
	fi, err = a.RootFileHandler.Stat(fullPath)
	//fi, err = os.Stat(fullPath)
	if fi != nil && fi.IsDir() {
		respondWithError(w, 400, "Can't stream a directory.\n", err, a.Logger)
		return
	}
	//file, err := os.Open(fullPath)
	file, err := a.RootFileHandler.Open(fullPath)
	w.Header().Set("Cache-Control", "no-cache")
	err, contentType := streamZipImages(fullPath, w, requestedPage, a.Logger)
	if err != nil {
		respondWithError(w, 500, "Error opening file.\n", err, a.Logger)
		return
	}
	defer file.Close()
	/*contentType, err := readContentType(file)
	if err != nil {
		log.Println(err)
		respondWithError(w, 500, "Internal error.", err)
		return
	}*/
	a.Logger.Debug(fmt.Sprintf("serving filetype %s", contentType),
		slog.String("component", "streamarchive"))
	//w.Header().Set("Content-Type", contentType)
	//http.ServeContent(w, r, file.Name(), fi.ModTime(), file) Ready method.
}

// POST /dev/remoteUpdate
func (a *ApiConfig) UpdateAndRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Access forbidden", nil, a.Logger)
		return
	}
	type response struct {
		Message string `json:"reply"`
	}
	logsDir := "./logs"
	if err := os.MkdirAll(logsDir, 0750); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Cannot create logs directory", err, a.Logger)
		return
	}
	respondWithJSON(w, 200, response{
		Message: "Executing server restart.",
	}, a.Logger)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	go func() {
		time.Sleep(100 * time.Millisecond)

		// Execute update completely detached
		if err := runUpdateDetached(); err != nil {
			a.Logger.Error("update failed",
				slog.String("component", "updateandrestart"),
				slog.String("error", fmt.Sprintf("%+v", err)))
		}
	}()

	go func() {
		time.Sleep(2 * time.Second)
		a.Logger.Info("server shutting down for an update")
		os.Exit(0)
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
