package functionality

import (
	"context"
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
)

func (a *ApiConfig) Login(w http.ResponseWriter, r *http.Request) {
	type loginParameters struct {
		Name      string `json:"name"`
		Password  string `json:"password"`
		WithToken bool   `json:"withToken"`
		Token     string `json:"token,omitempty"`
	}

	type response struct {
		Message string `json:"reply"`
		Token   string `json:"token,omitempty"`
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
	if params.WithToken {
		userName, err := auth.ValidateJWT(params.Token, a.Secret)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError,
				"Unexpected validation error. Token may be expired, please login.",
				err,
			)
			return
		}
		dbUser, err := a.Database.FindUser(context.Background(), userName)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, "Couldn't find user.", err)
			return
		}
		responseMsg := fmt.Sprintf("Succesfully logged in as %s\n", dbUser.Name)
		a.Authorized = true
		respondWithJSON(w, http.StatusOK, response{Message: responseMsg})

	} else {
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
		accessToken, err := auth.MakeJWT(dbUser.Name, a.Secret, time.Hour*24*7)
		responseMsg := fmt.Sprintf("Succesfully logged in as %s\n", dbUser.Name)
		params := database.UpdateUserTokenParams{Authtoken: accessToken, Name: dbUser.Name}
		a.Database.UpdateUserToken(context.Background(), params)
		a.Authorized = true
		respondWithJSON(w, http.StatusOK,
			response{
				Message: responseMsg,
				Token:   accessToken,
			},
		)
	}
}

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
	msg := fmt.Sprintf("Listing files in %s\n", fullPath)
	respondWithJSON(w, http.StatusOK,
		ListDirResponse{
			Message: msg,
			Files:   resultTree,
		},
	)
}

func (a *ApiConfig) StreamVideo(w http.ResponseWriter, r *http.Request) {
	type Response struct {
		Message string `json:"reply"`
	}
	requestPath := r.PathValue("path")
	log.Printf("Received a request with path %s.", requestPath)
	var fullPath string
	//fullPath := filepath.Join(a.CurrentRoot, r.PathValue("path"))
	if strings.Contains(requestPath, a.CurrentRoot) {
		fullPath = r.PathValue("path")
	} else {
		fullPath = filepath.Join(a.CurrentRoot, r.PathValue("path"))
	}
	log.Printf("Received a request to stream path %s\n", fullPath)
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
	log.Printf("Serving filetype %s", contentType)
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
		log.Printf("Path '%s' is not within allowed bounds of '%s'\n", absNewPath, absRoot)
		respondWithError(w, 403, "New root out of allowed bounds.", err)
		return
	}
	log.Printf("Received a request to move root to '%s'\n", newRoot)
	fi, err := os.Stat(newRoot)
	if err != nil {
		dir := filepath.Dir(filepath.Clean(newRoot))
		dirToFind := filepath.Base(newRoot)
		log.Printf("Finding optional matches for path %s.", dir)
		closestMatch, err := findClosestMatch(dir, dirToFind)
		if err != nil {
			respondWithError(w, 400, "Error finding new root.", err)
			return
		} else {
			log.Printf("Matched %s to %s", dirToFind, closestMatch)
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

func (a *ApiConfig) StreamArchive(w http.ResponseWriter, r *http.Request) {
	type Response struct {
		Message string `json:"reply"`
	}
	requestPath := r.PathValue("path")
	requestedPage, err := strconv.Atoi(r.URL.Query().Get("Page"))
	if err != nil {
		respondWithError(w, 500, "Error finding page.", err)
	}
	log.Printf("Received a request with path %s.", requestPath)
	var fullPath string
	//fullPath := filepath.Join(a.CurrentRoot, r.PathValue("path"))
	if strings.Contains(requestPath, a.CurrentRoot) {
		fullPath = r.PathValue("path")
	} else {
		fullPath = filepath.Join(a.CurrentRoot, r.PathValue("path"))
	}
	log.Printf("Received a request to stream image archive at %s\n", fullPath)
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
	log.Printf("Serving filetype %s", contentType)
	//w.Header().Set("Content-Type", contentType)
	//http.ServeContent(w, r, file.Name(), fi.ModTime(), file) Ready method.
	log.Println("Streaming full file.")
}

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
			log.Printf("Update failed: %v", err)
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
