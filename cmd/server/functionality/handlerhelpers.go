package functionality

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/finfreezer/homeserver/internal/database"
)

type FileNode struct {
	Name     string     `json:"name"`
	IsDir    bool       `json:"isDir"`
	Children []FileNode `json:"children,omitempty"`
}

type FileGroup map[string][]os.DirEntry

func buildFileTree(fromPath string, dirOnly bool, depth int, logger *slog.Logger) (FileNode, error) {
	itemList, err := os.ReadDir(fromPath)
	if err != nil {
		return FileNode{}, err
	}
	//itemList = sortFilesByNumber(itemList)
	itemList = doubleSort(itemList, logger)
	info, err := os.Stat(fromPath)
	if err != nil || !info.IsDir() {
		return FileNode{Name: info.Name(), IsDir: false}, err
	}
	currentDir := FileNode{Name: info.Name(), IsDir: true, Children: []FileNode{}}
	if dirOnly {
		for _, item := range itemList {
			if item.IsDir() {
				childNode := FileNode{}
				err = nil
				if depth > 0 {
					childNode, err = buildFileTree(path.Join(fromPath, item.Name()), dirOnly, depth-1, logger)
				} else {
					childNode.Name = item.Name()
					childNode.IsDir = item.IsDir()
					childNode.Children = nil
				}
				currentDir.Children = append(currentDir.Children, childNode)
				if err != nil {
					return childNode, err
				}
			}
		}
		return currentDir, nil

	} else {
		for _, item := range itemList {
			if !item.IsDir() {
				currentDir.Children = append(currentDir.Children,
					FileNode{
						Name:  item.Name(),
						IsDir: item.IsDir(),
					})

			} else {
				childNode := FileNode{}
				err = nil
				if depth > 0 {
					childNode, err = buildFileTree(path.Join(fromPath, item.Name()), dirOnly, depth-1, logger)
				} else {
					childNode.Name = item.Name()
					childNode.IsDir = item.IsDir()
					childNode.Children = nil
				}
				if err != nil {
					return childNode, err
				}
				currentDir.Children = append(currentDir.Children, childNode)
			}
		}
		return currentDir, nil
	}

}

func readByteRange(file *os.File, byteRange string, w http.ResponseWriter, logger *slog.Logger) (error, int) {
	fileInfo, err := file.Stat()
	fileSize := fileInfo.Size()
	if err != nil {
		return errors.New("Error getting fileinfo."), 500
	}

	if !strings.HasPrefix(byteRange, "bytes=") {
		return errors.New("No range found or corrupted header."), 416
	}
	trimmedByteRange := strings.TrimPrefix(byteRange, "bytes=")
	if start, end, found := strings.Cut(trimmedByteRange, "-"); !found {
		return errors.New("No range found or corrupted header."), 416

	} else {

		maxBytesPerWrite := 5 << 20
		writeData := make([]byte, maxBytesPerWrite)
		startInt, err := strconv.Atoi(start)
		if err != nil {
			logger.Error("error converting byte range",
				slog.String("starting bytes", start),
				slog.String("component", "readbyterange"),
			)
			return errors.New("Error converting range."), 416
		}

		if end == "" && startInt < int(fileSize) {
			w.Header().Set("Content-Length", strconv.Itoa(int(fileSize-int64(startInt)+1)))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", startInt, fileSize-1, fileSize))
			logger.Info("streaming full request with status 200",
				slog.String("component", "readbyterange"))
			w.WriteHeader(206)
			for n := startInt; n < int(fileSize); {
				nRead, err := file.ReadAt(writeData, int64(startInt))
				if nRead == 0 {
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					return nil, 0
				}
				startInt += nRead
				if err != nil && err != io.EOF {
					return err, 0
				}
				_, err = w.Write(writeData[:nRead])
				if err != nil {
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					return nil, 0
				}
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return err, 0
		} else {
			endInt, err := strconv.Atoi(end)
			if err != nil {
				logger.Error("error converting byte range",
					slog.String("starting bytes", end),
					slog.String("component", "readbyterange"),
				)
				return errors.New("Error converting range."), 416
			}
			if startInt < endInt && endInt < int(fileSize) {
				w.Header().Set("Content-Length", strconv.Itoa(int(endInt-startInt+1)))
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", startInt, endInt, fileSize))
				logger.Info("streaming byte range request with status 206",
					slog.String("component", "readbyterange"))
				w.WriteHeader(206)
				for n := startInt; n < endInt; {
					if endInt-n < len(writeData) {
						writeData = make([]byte, endInt-n-1)
					}
					//file.Seek(int64(startInt), 0)
					nRead, err := file.ReadAt(writeData, int64(n))
					if nRead == 0 {
						if flusher, ok := w.(http.Flusher); ok {
							flusher.Flush()
						}
						return nil, 0
					}
					n += nRead
					if err != nil && err != io.EOF {
						return err, 500
					}
					_, err = w.Write(writeData[:nRead])
					if err != nil {
						if flusher, ok := w.(http.Flusher); ok {
							flusher.Flush()
						}
						return nil, 0
					}
				}
				return err, 200
			} else {
				return errors.New("Invalid byte range."), 416
			}
		}
	}
}

func readContentType(f *os.File) (string, error) {
	readBuffer := make([]byte, 512)
	n, err := f.Read(readBuffer)
	f.Seek(0, 0)
	if err != nil && err != io.EOF && n != 0 {
		return "", err
	}
	return http.DetectContentType(readBuffer), nil
}

func createDefaultPlaylist(path string, a *ApiConfig, logger *slog.Logger) *os.File {
	logger.Debug(fmt.Sprintf("Creating a playlist with current directory for %s", path),
		slog.String("component", "createdefaultplaylist"))
	streamingPath := os.Getenv("DST_SERVER") + os.Getenv("DFLT_PORT") + "/stream/"
	startFrom := filepath.Base(path)
	logger.Debug(fmt.Sprintf("Start from episode '%s'", startFrom),
		slog.String("component", "createDefaultPlaylist"))
	dir := filepath.Dir(path)
	dirStat, err := os.Stat(dir)
	if err != nil {
		logger.Error("problem getting dirStat",
			slog.String("component", "createdefaultplaylist"),
			slog.String("error", fmt.Sprintf("%+v", err)),
		)
		return nil
	}
	if _, err := os.Stat(dir + "/playlist.m3u"); !errors.Is(err, os.ErrNotExist) {
		os.Remove(dir + "/playlist.m3u")
	}

	playlist, err := os.Create(dir + "/playlist.m3u")
	if err != nil {
		logger.Error("problem creating file",
			slog.String("component", "createdefaultplaylist"),
			slog.String("error", fmt.Sprintf("%+v", err)),
		)
		return nil
	}
	playlist.Write([]byte("#EXTM3U\n"))
	playlist.Write([]byte("#PLAYLIST: Streaming\n"))
	if dirStat.IsDir() {
		entryList, err := os.ReadDir(dir)
		if err != nil {
			logger.Error("problem reading directory",
				slog.String("component", "createdefaultplaylist"),
				slog.String("error", fmt.Sprintf("%+v", err)),
			)
			return nil
		}
		mediaEntryList := []os.DirEntry{}
		for _, entry := range entryList {
			if isValidType(entry.Name()) {
				mediaEntryList = append(mediaEntryList, entry)
			}
		}
		mediaEntryList = sortFilesByNumber(mediaEntryList, logger)
		mediaEntryList, episode := moveEpisodeToStart(mediaEntryList, startFrom)
		lastEp := len(mediaEntryList)
		for _, entry := range mediaEntryList {
			if episode > lastEp {
				episode = 1
			}
			infoStr := fmt.Sprintf("Episode %d", episode)
			urlPath, err := url.Parse(streamingPath + (cleanPath(dir+"/"+entry.Name(), a.CurrentRoot, logger)))
			if err != nil {
				logger.Error("error parsing url path",
					slog.String("component", "createdefaultplaylist"),
					slog.String("error", fmt.Sprintf("%+v", err)),
				)
				return nil
			}
			if !entry.IsDir() {
				toWrite := fmt.Sprintf("#EXTINF:-1,%s\n%s\n",
					infoStr, urlPath.String(),
				)
				playlist.Write([]byte(toWrite))
				episode += 1
			}
		}
		playlist.Seek(0, 0)
		return playlist
	} else {
		return nil
	}
}

func isValidType(name string) bool {
	validTypes := []string{".mp3", ".mp4", ".mkv", ".wav", ".avi", ".webm"}
	for _, typeName := range validTypes {
		if strings.Contains(name, typeName) {
			return true
		}
	}
	return false
}

func cleanPath(internal string, currentRoot string, logger *slog.Logger) string {
	logger.Debug(fmt.Sprintf("Working with internal path of %s, and root of %s.", internal, currentRoot),
		slog.String("component", "cleanPath"))
	pathList := strings.Split(internal, "/")
	rootList := strings.Split(currentRoot, "/")
	rootList[0] = "stream"
	pathList[0] = "stream"
	relativeRoot := path.Join(rootList...) + "/"
	newPath := path.Join(pathList...)
	logger.Debug(fmt.Sprintf("Moving %s to match %s", newPath, relativeRoot),
		slog.String("component", "cleanPath"))
	pathToReturn := strings.ReplaceAll(newPath, relativeRoot, "")
	return pathToReturn
}

func moveEpisodeToStart(files []os.DirEntry, firstEp string) ([]os.DirEntry, int) {
	firstIdx := 0
	newFiles := []os.DirEntry{}
	for i, file := range files {
		if strings.Contains(file.Name(), firstEp) {
			firstIdx = i
		}
	}
	newFiles = append(newFiles, files[firstIdx:]...)
	newFiles = append(newFiles, files[:firstIdx]...)
	return newFiles, firstIdx + 1
}

func servePlaylist(w http.ResponseWriter, fullPath string, a *ApiConfig) {
	fi, err := os.Stat(fullPath)
	if err != nil {
		a.Logger.Error("error reading path",
			slog.String("component", "serveplaylist"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		respondWithError(w, 400, "Couldn't reach target.\n", err, a.Logger)
		return
	}
	if fi.IsDir() {
		respondWithError(w, 400, "Can't stream a directory.\n", err, a.Logger)
		return
	}
	if playlist := createDefaultPlaylist(fullPath, a, a.Logger); playlist != nil {
		defer playlist.Close()
		a.Logger.Info("responding with a playlist",
			slog.String("component", "serveplaylist"))
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		w.Header().Set("Content-Disposition", "inline; filename=\"playlist.m3u\"")
		w.WriteHeader(200)
		io.Copy(w, playlist)
		return
	}
	respondWithError(w, 400, "Unable to reach target.", err, a.Logger)
}

func sortFilesByNumber(files []os.DirEntry, logger *slog.Logger) []os.DirEntry {
	//log.Println("Sorting files..")
	fileMap := make(map[int]os.DirEntry)
	fileNumbers := []int{}
	unNumberedFiles := []os.DirEntry{}
	for _, file := range files {
		fileNumber, err := findFileNumber(file.Name())
		if err != nil {
			logger.Error("error sorting files",
				slog.String("component", "sortfilesbynumber"),
				slog.String("error", fmt.Sprintf("%+v", err)))
			return files
		}
		if fileNumber == -1 {
			unNumberedFiles = append(unNumberedFiles, file)
		} else {
			if _, ok := fileMap[fileNumber]; !ok {
				fileMap[fileNumber] = file
			} else {
				for i := range 999 {
					fileNumber += i
					if _, ok := fileMap[fileNumber]; !ok {
						fileMap[fileNumber] = file
						break
					}
				}

			}
			fileNumbers = append(fileNumbers, fileNumber)
		}
	}
	return append(sortFileSlice(fileMap, fileNumbers), unNumberedFiles...)
}

func findFileNumber(filename string) (int, error) {
	//log.Println("Reading file numbers...")
	numberStart := -1
	numberEnd := -1
	for i, char := range filename {
		if unicode.IsNumber(char) {
			numberStart = i
			break
		}
	}
	if numberStart != -1 {
		for i, char := range filename[numberStart:] {
			if !unicode.IsNumber(char) {
				numberEnd = numberStart + i
				break
			}
		}
	}

	if numberStart == -1 || numberEnd == -1 {
		return -1, nil
	}
	//log.Printf("Number '%s' found for file %s.\n", filename[numberStart:numberEnd], filename)
	return strconv.Atoi(filename[numberStart:numberEnd])
}

func sortFileSlice(filemap map[int]os.DirEntry, fileslice []int) []os.DirEntry {
	//log.Println("Sorting slice indexes...")
	sort.Ints(fileslice)
	sortedFiles := make([]os.DirEntry, len(fileslice))
	for i, fileNumber := range fileslice {
		//log.Printf("Sorting %s to place %d.", filemap[fileNumber].Name(), i)
		sortedFiles[i] = filemap[fileNumber]
	}
	return sortedFiles
}

func findClosestMatch(dirPath, fileToMatch string) (string, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if strings.Contains(strings.ToLower(file.Name()), strings.ToLower(fileToMatch)) {
			return dirPath + "/" + file.Name(), err
		}
	}
	return "", errors.New("No matching files.")
}

func groupFilesByFirstChar(files []os.DirEntry) map[string][]os.DirEntry {
	groupMap := make(map[string][]os.DirEntry)
	for _, file := range files {
		firstLetter := strings.ToLower(string(file.Name()[0]))
		if _, ok := groupMap[firstLetter]; !ok {
			groupMap[firstLetter] = []os.DirEntry{file}
		} else {
			groupMap[firstLetter] = append(groupMap[firstLetter], file)
		}
	}
	return groupMap
}

func doubleSort(files []os.DirEntry, logger *slog.Logger) []os.DirEntry {
	groupMap := groupFilesByFirstChar(files)
	keySlice := []string{}
	overallFileSlice := []os.DirEntry{}
	for key := range groupMap {
		keySlice = append(keySlice, key)
	}
	sort.Strings(keySlice)

	for _, key := range keySlice {
		overallFileSlice = append(overallFileSlice, sortFilesByNumber(groupMap[key], logger)...)
	}
	return overallFileSlice
}

func streamZipImages(archivePath string, w http.ResponseWriter, requestedPage int, logger *slog.Logger) (error, string) {
	cache := []*zip.File{}
	contentType := ""
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err, ""
	}
	defer reader.Close()
	for _, f := range reader.File {
		cache = append(cache, f)
	}
	//Assume the call for 1st page is '1' -> 0th index of the cache.
	requestedPage -= 1
	finalPage := len(cache) - 1

	//Check limits.
	logger.Info(fmt.Sprintf("received a request for page #%d to be displayed", requestedPage),
		slog.String("component", "streamzipimages"))
	if requestedPage > finalPage {
		overflow := (requestedPage - finalPage)
		requestedPage = overflow
	}
	if requestedPage < 0 {
		overflow := math.Abs(float64(requestedPage))
		requestedPage = (finalPage - int(overflow))
	}
	if requestedPage < len(cache) {
		logger.Info(fmt.Sprintf("serving page #%d", requestedPage),
			slog.String("component", "streamzipimages"))
		imageReader, err := cache[requestedPage].Open()
		if err != nil {
			return err, ""
		}

		buf := make([]byte, 512)
		n, err := imageReader.Read(buf)
		contentType = http.DetectContentType(buf[:n])
		if err != nil && err != io.EOF {
			return err, ""
		}
		imageReader, _ = cache[requestedPage].Open()
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		if err != nil {
			return err, ""
		}

		_, err = io.Copy(w, imageReader)
		if err != nil {
			return err, ""
		}
	}
	return nil, contentType
}

func SetAuthCookie(w http.ResponseWriter, token string) {
	cookie := &http.Cookie{
		Name:     "jwt",
		Value:    token,
		Path:     "/",
		MaxAge:   7 * 24 * 60 * 60, // 7 days
		HttpOnly: true,
		Secure:   true, // Set to false for local development without HTTPS
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}

func ClearAuthCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     "jwt",
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // Delete immediately
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}

func GetAuthToken(r *http.Request) (string, error) {
	cookie, err := r.Cookie("jwt")
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

func GetSessionToken(r *http.Request) (string, error) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

func VerifySession(user database.User, session database.Session, logger *slog.Logger) bool {
	expiresUTC, err := time.Parse("02/01/2006 - 15:04", session.ExpiresAt)
	if err != nil {
		logger.Error("error parsing date",
			slog.String("component", "verifysession"),
			slog.String("error", fmt.Sprintf("%+v", err)))
		return false
	}
	if user.ID == session.UserID &&
		user.AuthtokenHash.Valid &&
		user.AuthtokenHash.String == session.TokenHash &&
		expiresUTC.After(time.Now().UTC()) &&
		!session.RevokedAt.Valid {
		return true
	}
	return false
}

type fileMatch struct {
	score float64
	file  fs.DirEntry
	path  string
}

func SearchForMatches(root string, searchTerm string, logger *slog.Logger) ([]fileMatch, error) {
	logger.Debug(fmt.Sprintf("Starting search from  %s", root),
		slog.String("component", "searchformatches"))
	matchDict := make(map[string]fileMatch)
	scoreDict := make(map[float64][]string)
	finalMatches := []fileMatch{}
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			name := strings.ToLower(d.Name())
			term := strings.ToLower(searchTerm)
			if strings.Contains(name, term) {
				score := calculateMatchScore(d.Name(), searchTerm, logger)
				matchDict[d.Name()] = fileMatch{
					score: score,
					file:  d,
					path:  path,
				}
				scoreDict[score] = append(scoreDict[score], d.Name())
			}
		}
		return nil
	})
	valueSlice := []float64{}
	for _, fileMatch := range matchDict {
		valueSlice = append(valueSlice, fileMatch.score)
	}
	sort.Float64s(valueSlice)
	slices.Reverse(valueSlice)
	valueSlice = slices.Compact(valueSlice)
	for _, value := range valueSlice {
		matches := scoreDict[value]
		for _, match := range matches {
			fileMatch := matchDict[match]
			finalMatches = append(finalMatches, fileMatch)
			if len(finalMatches) > 9 {
				break
			}
		}
		if len(finalMatches) > 9 {
			break
		}
	}
	if len(finalMatches) < 1 {
		return finalMatches, errors.New("No matches found.")
	}
	return finalMatches, nil
}

func calculateMatchScore(name string, searchTerm string, logger *slog.Logger) float64 {
	standardizedName := strings.ToLower(name)
	standardizedTerm := strings.ToLower(searchTerm)
	var totalScore float64 = 0.0
	if strings.Contains(standardizedName, standardizedTerm) {
		totalScore = 1.0 + (float64(len(standardizedTerm)) / float64(len(standardizedName)))
		/*if totalScore > 1.0 {
			totalScore = 1.0
		}*/
		return totalScore
	}
	fullParts := strings.Split(standardizedTerm, " ")
	maxPartialScore := 1.0 / float64(len(fullParts))
	logger.Debug(fmt.Sprintf("Max partial score of %.2f", maxPartialScore),
		slog.String("component", "calculatematchscore"))
	for _, part := range fullParts {
		if strings.Contains(standardizedName, part) {
			//adjustmentFactor := calcLengthAdjustment(standardizedName, part)
			totalScore += maxPartialScore * 0.8 //Adjust for only including partial terms separately.
		} else {
			for i := len(part) - 1; i > 0; i-- {
				if len(part[:i]) < 3 {
					totalScore += 0.0
					break
				}
				logger.Debug(fmt.Sprintf("Comparing %s to %s", part[:i], standardizedName),
					slog.String("component", "calculatematchscore"))
				if strings.Contains(standardizedName, part[:i]) {
					adjustmentFactor := calcLengthAdjustment(standardizedName, part[:i], part)
					totalScore += maxPartialScore * adjustmentFactor
					logger.Debug(fmt.Sprintf("Scoring term %s in %s as %.2f", standardizedName, part[:i], totalScore),
						slog.String("component", "calculatematchscore"))
					break
				}
			}
		}
	}
	return totalScore
}

func calcLengthAdjustment(original string, part string, fullPart string) float64 {
	start := strings.Index(original, part)
	matchingPart := original[start:]
	end := strings.Index(matchingPart, " ")
	if end == -1 {
		end = len(matchingPart) - 1
	}
	comparisonSlice := matchingPart[:end]
	partLengthFactor := float64(len(part)) / float64(len(fullPart))
	adjustmentFactor := (float64(len(part)) / float64(len(comparisonSlice))) * partLengthFactor
	return adjustmentFactor
}
