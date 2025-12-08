package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	OutlineAPIURL string
	OutlineAPIKey string
	ListenAddress string
	MetricsPath   string
	ScrapeTimeout time.Duration
	PageLimit     int
	Debug         bool
}

type Collection struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Document struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Text         string    `json:"text"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	PublishedAt  time.Time `json:"publishedAt"`
	ArchivedAt   time.Time `json:"archivedAt,omitempty"`
	DeletedAt    time.Time `json:"deletedAt,omitempty"`
	Views        int       `json:"views"`
	Revision     int       `json:"revision"`
	CollectionId string    `json:"collectionId"`
}

type User struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
}

type Pagination struct {
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
	NextPath string `json:"nextPath"`
}

type apiResp[T any] struct {
	Data       []T        `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type Exporter struct {
	config Config

	up                       *prometheus.Desc
	scrapeSuccessTimestamp   *prometheus.Desc
	scrapeErrorsTotal        prometheus.Counter
	scrapeDurationSeconds    prometheus.Gauge
	collectionsTotal         *prometheus.Desc
	collectionDocumentsCount *prometheus.Desc
	collectionAge            *prometheus.Desc
	documentsTotal           *prometheus.Desc
	documentRevisions        *prometheus.Desc
	documentViews            *prometheus.Desc
	documentAge              *prometheus.Desc
	documentSize             *prometheus.Desc
	documentUpdateAge        *prometheus.Desc
	usersTotal               *prometheus.Desc
	userLastActive           *prometheus.Desc
	userAge                  *prometheus.Desc
}

func newExporter(config Config) *Exporter {
	return &Exporter{
		config: config,
		up: prometheus.NewDesc(
			"outline_up",
			"Was the last Outline scrape successful",
			nil, nil),
		scrapeSuccessTimestamp: prometheus.NewDesc(
			"outline_scrape_success_timestamp",
			"Timestamp of the last successful scrape",
			nil, nil),
		scrapeErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "outline_scrape_errors_total",
			Help: "Total number of scrape errors",
		}),
		scrapeDurationSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outline_scrape_duration_seconds",
			Help: "Duration of the scrape",
		}),
		collectionsTotal: prometheus.NewDesc(
			"outline_collections_total",
			"Total number of collections",
			nil, nil),
		collectionDocumentsCount: prometheus.NewDesc(
			"outline_collection_documents_count",
			"Number of documents in a collection",
			[]string{"collection_id", "collection_name"}, nil),
		collectionAge: prometheus.NewDesc(
			"outline_collection_age_seconds",
			"Age of collection in seconds",
			[]string{"collection_id", "collection_name"}, nil),
		documentsTotal: prometheus.NewDesc(
			"outline_documents_total",
			"Total number of documents",
			nil, nil),
		documentRevisions: prometheus.NewDesc(
			"outline_document_revisions",
			"Number of revisions for a document",
			[]string{"document_id", "collection_id"}, nil),
		documentViews: prometheus.NewDesc(
			"outline_document_views",
			"Number of views for a document",
			[]string{"document_id", "collection_id"}, nil),
		documentAge: prometheus.NewDesc(
			"outline_document_age_seconds",
			"Age of document in seconds",
			[]string{"document_id", "collection_id"}, nil),
		documentSize: prometheus.NewDesc(
			"outline_document_size_bytes",
			"Size of document text in bytes",
			[]string{"document_id", "collection_id"}, nil),
		documentUpdateAge: prometheus.NewDesc(
			"outline_document_update_age_seconds",
			"Time since last document update in seconds",
			[]string{"document_id", "collection_id"}, nil),
		usersTotal: prometheus.NewDesc(
			"outline_users_total",
			"Total number of users",
			nil, nil),
		userLastActive: prometheus.NewDesc(
			"outline_user_last_active_seconds",
			"Time since user was last active in seconds",
			[]string{"user_id", "user_name"}, nil),
		userAge: prometheus.NewDesc(
			"outline_user_age_seconds",
			"Age of user account in seconds",
			[]string{"user_id", "user_name"}, nil),
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up
	ch <- e.scrapeSuccessTimestamp
	ch <- e.collectionsTotal
	ch <- e.collectionDocumentsCount
	ch <- e.collectionAge
	ch <- e.documentsTotal
	ch <- e.documentRevisions
	ch <- e.documentViews
	ch <- e.documentAge
	ch <- e.documentSize
	ch <- e.documentUpdateAge
	ch <- e.usersTotal
	ch <- e.userLastActive
	ch <- e.userAge
	e.scrapeErrorsTotal.Describe(ch)
	e.scrapeDurationSeconds.Describe(ch)
}

func (e *Exporter) debug(format string, args ...any) {
	if e.config.Debug {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func (e *Exporter) fetch(path string, target any, body any) error {
	maxRetries := 3
	baseDelay := time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			log.Printf("Retry %d/%d after %v for %s", attempt, maxRetries, delay, path)
			time.Sleep(delay)
		}

		err := e.doFetch(path, target, body)
		if err == nil {
			return nil
		}

		if attempt < maxRetries && (strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "timeout")) {
			e.debug("Retryable error: %v", err)
			continue
		}

		return err
	}

	return fmt.Errorf("max retries exceeded")
}

func (e *Exporter) doFetch(path string, target any, body any) error {
	client := &http.Client{Timeout: e.config.ScrapeTimeout}
	fullURL := e.config.OutlineAPIURL + path
	e.debug("POST %s", fullURL)

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewBuffer(bodyBytes)
		e.debug("Body: %s", string(bodyBytes))
	}

	req, err := http.NewRequest("POST", fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.config.OutlineAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if e.config.Debug {
		if dump, err := httputil.DumpRequestOut(req, true); err == nil {
			e.debug("REQUEST:\n%s", string(dump))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if e.config.Debug {
		if dump, err := httputil.DumpResponse(resp, false); err == nil {
			e.debug("RESPONSE:\n%s\n%s", string(dump), string(responseData))
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(responseData))
	}

	return json.Unmarshal(responseData, target)
}

func (e *Exporter) shouldPaginate(pagination Pagination, itemCount int) bool {
	hasNext := pagination.NextPath != ""
	nonEmpty := strings.TrimSpace(pagination.NextPath) != ""
	exactLimit := itemCount == pagination.Limit
	shouldContinue := hasNext && nonEmpty && exactLimit

	e.debug("Paginate: next=%s trim=%v exact=%v (%d==%d) => %v",
		pagination.NextPath, nonEmpty, exactLimit, itemCount, pagination.Limit, shouldContinue)
	return shouldContinue
}

func fetchAll[T any](exporter *Exporter, path string) ([]T, error) {
	var allItems []T
	exporter.debug("Fetch %s", path)

	var firstResponse apiResp[T]
	if err := exporter.fetch(path, &firstResponse, map[string]int{"limit": exporter.config.PageLimit, "offset": 0}); err != nil {
		return nil, fmt.Errorf("fetch first page: %w", err)
	}

	allItems = append(allItems, firstResponse.Data...)
	log.Printf("Fetched %d items (page 1)", len(firstResponse.Data))

	if !exporter.shouldPaginate(firstResponse.Pagination, len(firstResponse.Data)) {
		return allItems, nil
	}

	pageNumber := 1
	nextPath := firstResponse.Pagination.NextPath
	seenPaths := make(map[string]bool)
	seenPaths[path] = true

	for nextPath != "" && strings.TrimSpace(nextPath) != "" {
		if seenPaths[nextPath] {
			exporter.debug("Already seen path %s, stopping pagination", nextPath)
			break
		}
		seenPaths[nextPath] = true

		exporter.debug("Next: %s", nextPath)

		var response apiResp[T]
		if err := exporter.fetch(nextPath, &response, map[string]string{}); err != nil {
			return allItems, fmt.Errorf("fetch page %d: %w", pageNumber+1, err)
		}

		allItems = append(allItems, response.Data...)
		pageNumber++
		log.Printf("Fetched %d items (page %d, total %d)", len(response.Data), pageNumber, len(allItems))

		if !exporter.shouldPaginate(response.Pagination, len(response.Data)) {
			break
		}
		nextPath = response.Pagination.NextPath
	}

	log.Printf("Completed: %d items across %d pages", len(allItems), pageNumber)
	return allItems, nil
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	startTime := time.Now()
	success := true

	collections, err := fetchAll[Collection](e, "/api/collections.list")
	if err != nil {
		log.Printf("Error fetching collections: %v", err)
		e.scrapeErrorsTotal.Inc()
		success = false
	}

	documents, err := fetchAll[Document](e, "/api/documents.list")
	if err != nil {
		log.Printf("Error fetching documents: %v", err)
		e.scrapeErrorsTotal.Inc()
		success = false
	}

	users, err := fetchAll[User](e, "/api/users.list")
	if err != nil {
		log.Printf("Error fetching users: %v", err)
		e.scrapeErrorsTotal.Inc()
		success = false
	}

	if success {
		ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, 1)
		ch <- prometheus.MustNewConstMetric(e.scrapeSuccessTimestamp, prometheus.GaugeValue, float64(time.Now().Unix()))
	} else {
		ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, 0)
	}

	if len(collections) > 0 {
		ch <- prometheus.MustNewConstMetric(e.collectionsTotal, prometheus.GaugeValue, float64(len(collections)))

		documentCounts := make(map[string]int)
		for _, document := range documents {
			documentCounts[document.CollectionId]++
		}

		for _, collection := range collections {
			ch <- prometheus.MustNewConstMetric(e.collectionDocumentsCount, prometheus.GaugeValue,
				float64(documentCounts[collection.ID]), collection.ID, collection.Name)
			ch <- prometheus.MustNewConstMetric(e.collectionAge, prometheus.GaugeValue,
				time.Since(collection.CreatedAt).Seconds(), collection.ID, collection.Name)
		}
	}

	if len(documents) > 0 {
		uniqueDocuments := make(map[string]Document)
		for _, document := range documents {
			uniqueKey := document.ID + ":" + document.CollectionId
			if _, exists := uniqueDocuments[uniqueKey]; !exists {
				uniqueDocuments[uniqueKey] = document
			}
		}

		e.debug("Documents: total=%d unique=%d", len(documents), len(uniqueDocuments))
		if len(documents) != len(uniqueDocuments) {
			log.Printf("Warning: %d duplicate documents", len(documents)-len(uniqueDocuments))
		}

		ch <- prometheus.MustNewConstMetric(e.documentsTotal, prometheus.GaugeValue, float64(len(uniqueDocuments)))

		for _, document := range uniqueDocuments {
			ch <- prometheus.MustNewConstMetric(e.documentRevisions, prometheus.GaugeValue,
				float64(document.Revision), document.ID, document.CollectionId)
			ch <- prometheus.MustNewConstMetric(e.documentViews, prometheus.GaugeValue,
				float64(document.Views), document.ID, document.CollectionId)
			ch <- prometheus.MustNewConstMetric(e.documentAge, prometheus.GaugeValue,
				time.Since(document.CreatedAt).Seconds(), document.ID, document.CollectionId)
			ch <- prometheus.MustNewConstMetric(e.documentSize, prometheus.GaugeValue,
				float64(len(document.Text)), document.ID, document.CollectionId)
			ch <- prometheus.MustNewConstMetric(e.documentUpdateAge, prometheus.GaugeValue,
				time.Since(document.UpdatedAt).Seconds(), document.ID, document.CollectionId)
		}
	}

	if len(users) > 0 {
		ch <- prometheus.MustNewConstMetric(e.usersTotal, prometheus.GaugeValue, float64(len(users)))

		for _, user := range users {
			ch <- prometheus.MustNewConstMetric(e.userLastActive, prometheus.GaugeValue,
				time.Since(user.LastActiveAt).Seconds(), user.ID, user.Name)
			ch <- prometheus.MustNewConstMetric(e.userAge, prometheus.GaugeValue,
				time.Since(user.CreatedAt).Seconds(), user.ID, user.Name)
		}
	}

	e.scrapeDurationSeconds.Set(time.Since(startTime).Seconds())
	e.scrapeDurationSeconds.Collect(ch)
	e.scrapeErrorsTotal.Collect(ch)
}

func main() {
	config := Config{
		OutlineAPIURL: getEnv("OUTLINE_API_URL", "http://localhost:3000"),
		OutlineAPIKey: getEnv("OUTLINE_API_KEY", ""),
		ListenAddress: getEnv("LISTEN_ADDRESS", ":9877"),
		MetricsPath:   getEnv("METRICS_PATH", "/metrics"),
		ScrapeTimeout: getDuration("SCRAPE_TIMEOUT", 30*time.Second),
		PageLimit:     getInt("PAGE_LIMIT", 100),
		Debug:         getBool("DEBUG", false),
	}

	if config.OutlineAPIKey == "" {
		log.Fatal("OUTLINE_API_KEY environment variable is required")
	}

	exporter := newExporter(config)
	prometheus.MustRegister(exporter)

	http.Handle(config.MetricsPath, promhttp.Handler())
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Outline Wiki Exporter</title></head>
			<body>
			<h1>Outline Wiki Exporter</h1>
			<p><a href="` + config.MetricsPath + `">Metrics</a></p>
			</body>
			</html>`))
	})

	log.Printf("Starting Outline Wiki exporter on %s", config.ListenAddress)
	log.Printf("Using page limit of %d items", config.PageLimit)
	if config.Debug {
		log.Printf("Debug mode enabled")
	}
	log.Fatal(http.ListenAndServe(config.ListenAddress, nil))
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if value, ok := os.LookupEnv(key); ok {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
		log.Printf("Invalid duration %s=%s, using %s", key, value, fallback)
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
		log.Printf("Invalid int %s=%s, using %d", key, value, fallback)
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(value) {
		case "true", "1", "t", "yes", "y":
			return true
		case "false", "0", "f", "no", "n":
			return false
		}
		log.Printf("Invalid bool %s=%s, using %t", key, value, fallback)
	}
	return fallback
}
