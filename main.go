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

// Collection struct aligned with Outline API response
// Ref: https://www.getoutline.com/developers#tag/collections/POST/collections.list
type Collection struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// DocumentsCount removed as it's not returned by the API
}

// Document struct aligned with Outline API response
// Ref: https://www.getoutline.com/developers#tag/documents/POST/documents.list
type Document struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Text         string    `json:"text"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	PublishedAt  time.Time `json:"publishedAt"`
	ArchivedAt   time.Time `json:"archivedAt,omitempty"`
	DeletedAt    time.Time `json:"deletedAt,omitempty"`
	Views        int       `json:"views"`    // Changed from ViewCount as per API
	Revision     int       `json:"revision"` // Changed from RevisionCount as per API
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

type CollectionsResponse struct {
	Data       []Collection `json:"data"`
	Pagination Pagination   `json:"pagination"`
}

type DocumentsResponse struct {
	Data       []Document `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type UsersResponse struct {
	Data       []User     `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type OutlineExporter struct {
	config Config

	up                     *prometheus.Desc
	scrapeSuccessTimestamp *prometheus.Desc
	scrapeErrorsTotal      prometheus.Counter
	scrapeDurationSeconds  prometheus.Gauge

	collectionsTotal         *prometheus.Desc
	collectionDocumentsCount *prometheus.Desc
	collectionAge            *prometheus.Desc

	documentsTotal    *prometheus.Desc
	documentRevisions *prometheus.Desc
	documentViews     *prometheus.Desc
	documentAge       *prometheus.Desc
	documentSize      *prometheus.Desc
	documentUpdateAge *prometheus.Desc

	usersTotal *prometheus.Desc
	// Metrics userLastActive and userAge sont supprimées de la structure mais sont toujours collectées
}

func NewOutlineExporter(config Config) *OutlineExporter {
	return &OutlineExporter{
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

		// Les métriques userLastActive et userAge sont complètement supprimées
	}
}

func (e *OutlineExporter) Describe(ch chan<- *prometheus.Desc) {
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
	// Les métriques userLastActive et userAge sont supprimées de Describe()

	e.scrapeErrorsTotal.Describe(ch)
	e.scrapeDurationSeconds.Describe(ch)
}

// Debug logging helper
func (e *OutlineExporter) debugLog(format string, v ...interface{}) {
	if e.config.Debug {
		log.Printf("[DEBUG] "+format, v...)
	}
}

// fetchPage is a general purpose method to fetch data from the Outline API
// Always uses POST as per Outline API requirements, with optional request body
func (e *OutlineExporter) fetchPage(path string, target interface{}, bodyData interface{}) error {
	client := &http.Client{
		Timeout: e.config.ScrapeTimeout,
	}

	// Always use POST method as per Outline API docs
	method := "POST"
	fullURL := e.config.OutlineAPIURL + path
	e.debugLog("Making %s request to: %s", method, fullURL)

	// Prepare request body if provided
	var body io.Reader
	if bodyData != nil {
		bodyBytes, err := json.Marshal(bodyData)
		if err != nil {
			return fmt.Errorf("error marshaling request body: %w", err)
		}
		body = bytes.NewBuffer(bodyBytes)
		e.debugLog("Request body: %s", string(bodyBytes))
	}

	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.config.OutlineAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Dump request for debugging if enabled
	if e.config.Debug {
		reqDump, err := httputil.DumpRequestOut(req, true)
		if err != nil {
			e.debugLog("Error dumping request: %v", err)
		} else {
			e.debugLog("REQUEST:\n%s", string(reqDump))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error executing request: %w", err)
	}
	defer resp.Body.Close()

	// Dump response for debugging if enabled
	if e.config.Debug {
		respDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			e.debugLog("Error dumping response: %v", err)
		} else {
			e.debugLog("RESPONSE:\n%s", string(respDump))
		}
	}

	// Read the full response body for error reporting and parsing
	bodyContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status code %d: %s", resp.StatusCode, string(bodyContent))
	}

	// Parse the JSON response
	err = json.Unmarshal(bodyContent, target)
	if err != nil {
		return fmt.Errorf("error parsing response: %w", err)
	}

	return nil
}

// Helper function to check if the pagination data should be used
func (e *OutlineExporter) shouldPaginate(paginationData Pagination, itemCount int) bool {
	hasNextPath := paginationData.NextPath != ""
	nonEmptyPath := strings.TrimSpace(paginationData.NextPath) != ""
	exactLimit := itemCount == paginationData.Limit

	e.debugLog("Pagination analysis:")
	e.debugLog("  - Has nextPath: %v (%s)", hasNextPath, paginationData.NextPath)
	e.debugLog("  - Non-empty path: %v", nonEmptyPath)
	e.debugLog("  - Got exact limit: %v (got %d, limit was %d)", exactLimit, itemCount, paginationData.Limit)
	e.debugLog("  - Should paginate: %v", hasNextPath && nonEmptyPath && exactLimit)

	// Only paginate if all conditions are met
	return hasNextPath && nonEmptyPath && exactLimit
}

// fetchAllCollections fetches all collections via Outline API
// Reference: https://www.getoutline.com/developers#tag/collections/POST/collections.list
func (e *OutlineExporter) fetchAllCollections() ([]Collection, error) {
	var allCollections []Collection
	path := "/api/collections.list"

	e.debugLog("Starting collection fetch with path: %s", path)

	// Initial request body with pagination parameters
	requestBody := map[string]int{
		"limit":  e.config.PageLimit,
		"offset": 0,
	}

	// Initial request
	var initialResponse CollectionsResponse
	err := e.fetchPage(path, &initialResponse, requestBody)
	if err != nil {
		return nil, fmt.Errorf("error fetching initial page of collections: %w", err)
	}

	// Add the collections from the first page
	allCollections = append(allCollections, initialResponse.Data...)
	log.Printf("Fetched %d collections in first page", len(initialResponse.Data))

	// Dump full pagination data for debugging
	paginationBytes, _ := json.MarshalIndent(initialResponse.Pagination, "", "  ")
	e.debugLog("Initial pagination data: %s", string(paginationBytes))

	// Determine if we need pagination
	if !e.shouldPaginate(initialResponse.Pagination, len(initialResponse.Data)) {
		log.Printf("No more collections to fetch (total: %d)", len(allCollections))
		return allCollections, nil
	}

	// Follow pagination to get remaining pages
	pageCount := 1
	nextPath := initialResponse.Pagination.NextPath

	for nextPath != "" && strings.TrimSpace(nextPath) != "" {
		e.debugLog("Following nextPath: %s", nextPath)

		var response CollectionsResponse
		// For pagination requests, we still need to use POST but with empty body
		// as per Outline API requirements
		err := e.fetchPage(nextPath, &response, map[string]string{})
		if err != nil {
			// Return what we have so far with the error
			return allCollections, fmt.Errorf("error fetching page %d of collections: %w", pageCount+1, err)
		}

		// Dump pagination data for this page
		paginationBytes, _ := json.MarshalIndent(response.Pagination, "", "  ")
		e.debugLog("Page %d pagination data: %s", pageCount+1, string(paginationBytes))

		allCollections = append(allCollections, response.Data...)
		pageCount++
		log.Printf("Fetched %d collections in page %d, %d total so far",
			len(response.Data), pageCount, len(allCollections))

		// Only continue if we should paginate
		if !e.shouldPaginate(response.Pagination, len(response.Data)) {
			e.debugLog("Stopping pagination after page %d", pageCount)
			break
		}
		nextPath = response.Pagination.NextPath
	}

	log.Printf("Completed fetching collections: %d items across %d pages", len(allCollections), pageCount)
	return allCollections, nil
}

// fetchAllDocuments fetches all documents via Outline API
// Reference: https://www.getoutline.com/developers#tag/documents/POST/documents.list
func (e *OutlineExporter) fetchAllDocuments() ([]Document, error) {
	var allDocuments []Document
	path := "/api/documents.list"

	e.debugLog("Starting documents fetch with path: %s", path)

	// Initial request body with pagination parameters
	requestBody := map[string]int{
		"limit":  e.config.PageLimit,
		"offset": 0,
	}

	// Initial request
	var initialResponse DocumentsResponse
	err := e.fetchPage(path, &initialResponse, requestBody)
	if err != nil {
		return nil, fmt.Errorf("error fetching initial page of documents: %w", err)
	}

	// Add the documents from the first page
	allDocuments = append(allDocuments, initialResponse.Data...)
	log.Printf("Fetched %d documents in first page", len(initialResponse.Data))

	// Dump full pagination data for debugging
	paginationBytes, _ := json.MarshalIndent(initialResponse.Pagination, "", "  ")
	e.debugLog("Initial pagination data: %s", string(paginationBytes))

	// Determine if we need pagination
	if !e.shouldPaginate(initialResponse.Pagination, len(initialResponse.Data)) {
		log.Printf("No more documents to fetch (total: %d)", len(allDocuments))
		return allDocuments, nil
	}

	// Follow pagination to get remaining pages
	pageCount := 1
	nextPath := initialResponse.Pagination.NextPath

	for nextPath != "" && strings.TrimSpace(nextPath) != "" {
		e.debugLog("Following nextPath: %s", nextPath)

		var response DocumentsResponse
		// For pagination requests, we still need to use POST but with empty body
		err := e.fetchPage(nextPath, &response, map[string]string{})
		if err != nil {
			// Return what we have so far with the error
			return allDocuments, fmt.Errorf("error fetching page %d of documents: %w", pageCount+1, err)
		}

		// Dump pagination data for this page
		paginationBytes, _ := json.MarshalIndent(response.Pagination, "", "  ")
		e.debugLog("Page %d pagination data: %s", pageCount+1, string(paginationBytes))

		allDocuments = append(allDocuments, response.Data...)
		pageCount++
		log.Printf("Fetched %d documents in page %d, %d total so far",
			len(response.Data), pageCount, len(allDocuments))

		// Only continue if we should paginate
		if !e.shouldPaginate(response.Pagination, len(response.Data)) {
			e.debugLog("Stopping pagination after page %d", pageCount)
			break
		}
		nextPath = response.Pagination.NextPath
	}

	log.Printf("Completed fetching documents: %d items across %d pages", len(allDocuments), pageCount)
	return allDocuments, nil
}

// fetchAllUsers fetches all users via Outline API
func (e *OutlineExporter) fetchAllUsers() ([]User, error) {
	var allUsers []User
	path := "/api/users.list"

	e.debugLog("Starting users fetch with path: %s", path)

	// Initial request body with pagination parameters
	requestBody := map[string]int{
		"limit":  e.config.PageLimit,
		"offset": 0,
	}

	// Initial request
	var initialResponse UsersResponse
	err := e.fetchPage(path, &initialResponse, requestBody)
	if err != nil {
		return nil, fmt.Errorf("error fetching initial page of users: %w", err)
	}

	// Add the users from the first page
	allUsers = append(allUsers, initialResponse.Data...)
	log.Printf("Fetched %d users in first page", len(initialResponse.Data))

	// Dump full pagination data for debugging
	paginationBytes, _ := json.MarshalIndent(initialResponse.Pagination, "", "  ")
	e.debugLog("Initial pagination data: %s", string(paginationBytes))

	// Determine if we need pagination
	if !e.shouldPaginate(initialResponse.Pagination, len(initialResponse.Data)) {
		log.Printf("No more users to fetch (total: %d)", len(allUsers))
		return allUsers, nil
	}

	// Follow pagination to get remaining pages
	pageCount := 1
	nextPath := initialResponse.Pagination.NextPath

	for nextPath != "" && strings.TrimSpace(nextPath) != "" {
		e.debugLog("Following nextPath: %s", nextPath)

		var response UsersResponse
		// For pagination requests, we still need to use POST but with empty body
		err := e.fetchPage(nextPath, &response, map[string]string{})
		if err != nil {
			// Return what we have so far with the error
			return allUsers, fmt.Errorf("error fetching page %d of users: %w", pageCount+1, err)
		}

		// Dump pagination data for this page
		paginationBytes, _ := json.MarshalIndent(response.Pagination, "", "  ")
		e.debugLog("Page %d pagination data: %s", pageCount+1, string(paginationBytes))

		allUsers = append(allUsers, response.Data...)
		pageCount++
		log.Printf("Fetched %d users in page %d, %d total so far",
			len(response.Data), pageCount, len(allUsers))

		// Only continue if we should paginate
		if !e.shouldPaginate(response.Pagination, len(response.Data)) {
			e.debugLog("Stopping pagination after page %d", pageCount)
			break
		}
		nextPath = response.Pagination.NextPath
	}

	log.Printf("Completed fetching users: %d items across %d pages", len(allUsers), pageCount)
	return allUsers, nil
}

// Collect implements the prometheus.Collector interface
func (e *OutlineExporter) Collect(ch chan<- prometheus.Metric) {
	startTime := time.Now()
	var success bool = true

	// Fetch all collections using pagination
	collections, err := e.fetchAllCollections()
	if err != nil {
		log.Printf("Error fetching collections: %v", err)
		e.scrapeErrorsTotal.Inc()
		success = false
	}

	// Fetch all documents using pagination
	documents, err := e.fetchAllDocuments()
	if err != nil {
		log.Printf("Error fetching documents: %v", err)
		e.scrapeErrorsTotal.Inc()
		success = false
	}

	// Fetch all users using pagination
	users, err := e.fetchAllUsers()
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

	// Export collections metrics if we have data
	if len(collections) > 0 {
		ch <- prometheus.MustNewConstMetric(e.collectionsTotal, prometheus.GaugeValue, float64(len(collections)))

		// Calculate document counts per collection by processing all documents
		collectionDocCounts := make(map[string]int)

		// Count documents per collection by iterating through documents
		for _, doc := range documents {
			collectionDocCounts[doc.CollectionId]++
		}

		// Now export metrics for each collection
		for _, collection := range collections {
			// Get document count for this collection
			docCount := collectionDocCounts[collection.ID]

			ch <- prometheus.MustNewConstMetric(
				e.collectionDocumentsCount,
				prometheus.GaugeValue,
				float64(docCount), // Use the calculated count
				collection.ID,
				collection.Name,
			)

			ch <- prometheus.MustNewConstMetric(
				e.collectionAge,
				prometheus.GaugeValue,
				time.Since(collection.CreatedAt).Seconds(),
				collection.ID,
				collection.Name,
			)
		}
	} else {
		log.Printf("No collections data to export")
	}

	// Export documents metrics if we have data
	if len(documents) > 0 {
		ch <- prometheus.MustNewConstMetric(e.documentsTotal, prometheus.GaugeValue, float64(len(documents)))

		for _, doc := range documents {
			ch <- prometheus.MustNewConstMetric(
				e.documentRevisions,
				prometheus.GaugeValue,
				float64(doc.Revision), // Changed from RevisionCount to Revision as per API
				doc.ID,
				doc.CollectionId,
			)

			ch <- prometheus.MustNewConstMetric(
				e.documentViews,
				prometheus.GaugeValue,
				float64(doc.Views), // Changed from ViewCount to Views as per API
				doc.ID,
				doc.CollectionId,
			)

			ch <- prometheus.MustNewConstMetric(
				e.documentAge,
				prometheus.GaugeValue,
				time.Since(doc.CreatedAt).Seconds(),
				doc.ID,
				doc.CollectionId,
			)

			ch <- prometheus.MustNewConstMetric(
				e.documentSize,
				prometheus.GaugeValue,
				float64(len(doc.Text)),
				doc.ID,
				doc.CollectionId,
			)

			ch <- prometheus.MustNewConstMetric(
				e.documentUpdateAge,
				prometheus.GaugeValue,
				time.Since(doc.UpdatedAt).Seconds(),
				doc.ID,
				doc.CollectionId,
			)
		}
	} else {
		log.Printf("No documents data to export")
	}

	// Export users metrics if we have data
	if len(users) > 0 {
		ch <- prometheus.MustNewConstMetric(e.usersTotal, prometheus.GaugeValue, float64(len(users)))

		// Les métriques userLastActive et userAge sont complètement supprimées de Collect()
		// Nous continuons à collecter les données users mais nous n'exportons pas les métriques détaillées par utilisateur
	} else {
		log.Printf("No users data to export")
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
		ScrapeTimeout: getDurationEnv("SCRAPE_TIMEOUT", 10*time.Second),
		PageLimit:     getIntEnv("PAGE_LIMIT", 25),
		Debug:         getBoolEnv("DEBUG", false),
	}

	if config.OutlineAPIKey == "" {
		log.Fatal("OUTLINE_API_KEY environment variable is required")
	}

	exporter := NewOutlineExporter(config)
	prometheus.MustRegister(exporter)

	http.Handle(config.MetricsPath, promhttp.Handler())
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

// Helper functions for environment variables
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if value, ok := os.LookupEnv(key); ok {
		duration, err := time.ParseDuration(value)
		if err == nil {
			return duration
		}
		log.Printf("Invalid duration for %s: %s, using default: %s", key, value, fallback)
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		intValue, err := parseInt(value)
		if err == nil {
			return intValue
		}
		log.Printf("Invalid integer for %s: %s, using default: %d", key, value, fallback)
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(value) {
		case "true", "1", "t", "yes", "y":
			return true
		case "false", "0", "f", "no", "n":
			return false
		}
		log.Printf("Invalid boolean for %s: %s, using default: %t", key, value, fallback)
	}
	return fallback
}

func parseInt(value string) (int, error) {
	var intValue int
	_, err := fmt.Sscanf(value, "%d", &intValue)
	return intValue, err
}
