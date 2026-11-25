package functionality

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/finfreezer/homeserver/internal/auth"
)

const (
	maxRequestsPerIP   int = 50
	defaultLimiterTime     = time.Minute * 30
)

type middleware func(http.Handler) http.Handler
type LimiterClient struct {
	Now       func() time.Time
	ExpiresIn time.Time
	Key       string
	Left      int
}
type Limiter struct {
	KeyMap map[string]*LimiterClient
}

var (
	LimitMap        = make(map[string]*LimiterClient)
	LimitedRequests = Limiter{KeyMap: LimitMap}
)

func ApplyMiddleware(handler http.Handler, middlewareChain ...middleware) http.Handler {
	for _, currentMiddleware := range slices.Backward(middlewareChain) {
		handler = currentMiddleware(handler)
	}
	return handler
}

func CheckClearance(next http.Handler, conf *ApiConfig) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			log.Println("Verifying authorization")
			token, err := GetSessionToken(r)
			if err != nil {
				log.Println(err)
				respondWithError(w, 403, "No active session found.", err)
				return
			}
			session, err := conf.Database.GetSession(r.Context(), auth.HashSessionToken(token))
			if err != nil {
				log.Println(err)
				respondWithError(w, 403, "No active session found.", err)
				return
			}
			user, err := conf.Database.FindUserByID(r.Context(), session.UserID)
			if err != nil {
				log.Println(err)
				respondWithError(w, 403, "No matching user found.", err)
				return
			}
			if VerifySession(user, session, conf.Logger) && user.IsAdmin == 1 {
				log.Println("Served admin request.")
				next.ServeHTTP(w, r)
				return
			}
			respondWithJSON(w, 403, "Missing permissions or invalid session.")
		})
}

func BlockCSRF(expectedOrigin string) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			if request.Method != "POST" {
				next.ServeHTTP(responseWriter, request)
				return
			}
			if request.Method == "POST" && request.URL.Path == "/integrations/pawpal/webhook" {
				next.ServeHTTP(responseWriter, request)
				return
			}
			origin := request.Header.Get("Origin")
			log.Printf("Serving request from Origin %s. Expecting origin %s\n", origin, expectedOrigin)
			var referer string
			if origin == "" {
				referer = request.Header.Get("Referer")
				if !strings.Contains(referer, expectedOrigin) {
					responseWriter.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(responseWriter, request)
				return
			}
			if origin != expectedOrigin {
				responseWriter.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(responseWriter, request)
		})
	}
}

func IncludeSecureHeaders(backendorigin string) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			policies := fmt.Sprintf("default-src 'self'; script-src 'self'; style-src 'self'; media-src '%s'; img-src '%s' data:; frame-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'self'", backendorigin, backendorigin)
			responseWriter.Header().Add("Content-Security-Policy", policies)
			responseWriter.Header().Add("X-Frame-Options", "SAMEORIGIN")
			responseWriter.Header().Add("Referrer-Policy", "strict-origin-when-cross-origin")
			responseWriter.Header().Set("X-Content-Type-Options", "nosniff")
			responseWriter.Header().Add("Cross-Origin-Opener-Policy", "same-origin")
			responseWriter.Header().Add("Origin-Agent-Cluster", "?1")
			responseWriter.Header().Add("X-DNS-Prefetch-Control", "off")
			responseWriter.Header().Add("X-Download-Options", "noopen")
			responseWriter.Header().Add("X-Permitted-Cross-Domain-Policies", "none")
			responseWriter.Header().Add("X-XSS-Protection", "0")
			next.ServeHTTP(responseWriter, request)
		})
	}
}

func RateLimitRequests() middleware {
	type response struct {
		Message string `json:"reply"`
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("Serving a request to %s\n", r.URL.Path)
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				log.Println(err)
				respondWithError(w, 400, "Unable to verify connection.", err)
				return
			}
			if strings.Contains(r.URL.Path, "verify") || strings.Contains(r.URL.Path, "Entries") {
				next.ServeHTTP(w, r)
				return
			}
			if limit, ok := LimitedRequests.KeyMap[host]; ok {
				log.Printf("Serving session %+v\n", limit)
				if limit.Now().UTC().After(limit.ExpiresIn) {
					limit.Left = maxRequestsPerIP
					limit.ExpiresIn = limit.Now().UTC().Add(defaultLimiterTime)
				}
				limit.Left -= 1
				if limit.Left < 1 {
					limitRefresh := time.Until(limit.ExpiresIn)
					limitSeconds := int(limitRefresh.Seconds())
					limitSecondsStr := strconv.Itoa(limitSeconds)
					w.Header().Add("Retry-After", limitSecondsStr)
					msg := fmt.Sprintf("Too many requests. Try again in %s seconds.", limitSecondsStr)
					respondWithJSON(w, 429, response{Message: msg})
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			log.Printf("Serving new requests to client IP: %s\n", redactFullIP(host))
			//log.Printf("Serving new requests to client IP: %s\n", host)
			newClient := LimiterClient{
				Now:       time.Now().UTC,
				ExpiresIn: time.Now().UTC().Add(defaultLimiterTime),
				Key:       host,
				Left:      maxRequestsPerIP,
			}
			LimitedRequests.KeyMap[host] = &newClient
			next.ServeHTTP(w, r)
		})
	}
}

func redactFullIP(address string) string {
	parts := strings.Split(address, ".")
	if len(parts) != 4 {
		return address
	}
	return fmt.Sprintf("%s.%s.***.***", parts[0], parts[1])
}
