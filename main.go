package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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
}

type Collection struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	DocumentsCount int       `json:"documentsCount"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Document struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Text          string    `json:"text"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ViewCount     int       `json:"viewCount"`
	RevisionCount int       `json:"revisionCount"`
	CollectionId  string    `json:"collectionId"`
}

type User struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"createdAt"`
	LastActiveAt time.Time `json:"lastActiveAt"`
}

type CollectionsResponse struct {
	Data []Collection `json:"data"`
}

type DocumentsResponse struct {
	Data []Document `json:"data"`
}

type UsersResponse struct {
	Data []User `json:"data"`
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

	usersTotal     *prometheus.Desc
	userLastActive *prometheus.Desc
	userAge        *prometheus.Desc
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
	ch <- e.userLastActive
	ch <- e.userAge

	e.scrapeErrorsTotal.Describe(ch)
	e.scrapeDurationSeconds.Describe(ch)
}

func (e *OutlineExporter) fetchData(path string, target interface{}) error {
	client := &http.Client{
		Timeout: e.config.ScrapeTimeout,
	}

	req, err := http.NewRequest("POST", e.config.OutlineAPIURL+path, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+e.config.OutlineAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status code %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func (e *OutlineExporter) Collect(ch chan<- prometheus.Metric) {
	startTime := time.Now()

	var collections CollectionsResponse
	var documents DocumentsResponse
	var users UsersResponse
	var success bool = true

	err := e.fetchData("/api/collections.list", &collections)
	if err != nil {
		log.Printf("Error fetching collections: %v", err)
		e.scrapeErrorsTotal.Inc()
		success = false
	}

	err = e.fetchData("/api/documents.list", &documents)
	if err != nil {
		log.Printf("Error fetching documents: %v", err)
		e.scrapeErrorsTotal.Inc()
		success = false
	}

	err = e.fetchData("/api/users.list", &users)
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

	ch <- prometheus.MustNewConstMetric(e.collectionsTotal, prometheus.GaugeValue, float64(len(collections.Data)))

	for _, collection := range collections.Data {
		ch <- prometheus.MustNewConstMetric(
			e.collectionDocumentsCount,
			prometheus.GaugeValue,
			float64(collection.DocumentsCount),
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

	ch <- prometheus.MustNewConstMetric(e.documentsTotal, prometheus.GaugeValue, float64(len(documents.Data)))

	for _, doc := range documents.Data {

		ch <- prometheus.MustNewConstMetric(
			e.documentRevisions,
			prometheus.GaugeValue,
			float64(doc.RevisionCount),
			doc.ID,
			doc.CollectionId,
		)

		ch <- prometheus.MustNewConstMetric(
			e.documentViews,
			prometheus.GaugeValue,
			float64(doc.ViewCount),
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

	ch <- prometheus.MustNewConstMetric(e.usersTotal, prometheus.GaugeValue, float64(len(users.Data)))

	for _, user := range users.Data {
		ch <- prometheus.MustNewConstMetric(
			e.userLastActive,
			prometheus.GaugeValue,
			time.Since(user.LastActiveAt).Seconds(),
			user.ID,
			user.Name,
		)

		ch <- prometheus.MustNewConstMetric(
			e.userAge,
			prometheus.GaugeValue,
			time.Since(user.CreatedAt).Seconds(),
			user.ID,
			user.Name,
		)
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
		ScrapeTimeout: getDurationEnv("SCRAPE_TIMEOUT", 5*time.Second),
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
	log.Fatal(http.ListenAndServe(config.ListenAddress, nil))
}

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
