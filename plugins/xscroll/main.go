package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	pb "github.com/fipso/chadbot/gen/chadbot"
	"github.com/fipso/chadbot/pkg/sdk"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

//go:embed PLUGIN.md
var pluginDocumentation string

const (
	pluginName    = "xscroll"
	pluginVersion = "1.0.0"
	pluginDesc    = "Scroll through X.com (Twitter) with human-like behavior and extract tweets"

	tweetsTable = "tweets"

	// Default values
	defaultDebugPort        = 9222
	defaultScrollIntervalMs = 2000
	defaultRandomnessFactor = 0.3

	// Retention period
	retentionDays = 7
)

var (
	client  *sdk.Client
	storage *sdk.StorageClient

	// Scroll session state
	sessionMu      sync.Mutex
	scrollCtx      context.Context
	scrollCancel   context.CancelFunc
	scrollRunning  bool
	scrollStats    ScrollStats
	seenTweetIDs   = make(map[string]bool) // Track seen tweets in current session
	seenTweetIDsMu sync.RWMutex
)

// Tweet represents an extracted tweet
type Tweet struct {
	ID           string   `json:"id"`
	URL          string   `json:"url"`
	AuthorName   string   `json:"author_name"`
	AuthorHandle string   `json:"author_handle"`
	Content      string   `json:"content"`
	Timestamp    string   `json:"timestamp"`
	Images       []string `json:"images"`
	Videos       []string `json:"videos"`
	ExtractedAt  int64    `json:"extracted_at"`
}

// ScrollStats tracks scrolling session statistics
type ScrollStats struct {
	StartedAt      time.Time `json:"started_at"`
	TweetsFound    int       `json:"tweets_found"`
	ScrollCount    int       `json:"scroll_count"`
	DebugPort      int       `json:"debug_port"`
	ScrollInterval int       `json:"scroll_interval_ms"`
	Randomness     float64   `json:"randomness_factor"`
}

func main() {
	log.SetPrefix("[XScroll] ")
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client = sdk.NewClient(pluginName, pluginVersion, pluginDesc)
	client = client.SetDocumentation(pluginDocumentation)

	// Register skills
	registerSkills()

	if err := client.Connect(ctx); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	log.Println("Plugin registered successfully")

	// Initialize storage
	storage = client.Storage()
	if err := initStorage(); err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Start cleanup goroutine
	go cleanupRoutine(ctx)

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		stopScrolling()
		cancel()
	}()

	if err := client.Run(ctx); err != nil && err != context.Canceled {
		log.Printf("Client error: %v", err)
	}
	client.Close()
}

func initStorage() error {
	columns := []*pb.ColumnDef{
		{Name: "id", Type: "TEXT", PrimaryKey: true},
		{Name: "url", Type: "TEXT"},
		{Name: "author_name", Type: "TEXT"},
		{Name: "author_handle", Type: "TEXT"},
		{Name: "content", Type: "TEXT"},
		{Name: "timestamp", Type: "TEXT"},
		{Name: "images", Type: "TEXT"},       // JSON array
		{Name: "videos", Type: "TEXT"},       // JSON array
		{Name: "extracted_at", Type: "INTEGER"},
	}

	if err := storage.CreateTable(tweetsTable, columns, true); err != nil {
		return fmt.Errorf("failed to create tweets table: %w", err)
	}

	log.Println("Storage initialized")
	return nil
}

func registerSkills() {
	// xscroll_start - Start scrolling and extracting
	client.RegisterSkill(&pb.Skill{
		Name:        "xscroll_start",
		Description: "Start scrolling X.com/Twitter and extracting tweets. Requires Chrome running with --remote-debugging-port",
		Parameters: []*pb.SkillParameter{
			{Name: "debug_port", Type: "number", Description: "Chrome debug port (default: 9222)", Required: false},
			{Name: "scroll_interval_ms", Type: "number", Description: "Base interval between scrolls in milliseconds (default: 2000)", Required: false},
			{Name: "randomness_factor", Type: "number", Description: "Randomness factor 0-1 for scroll timing (default: 0.3)", Required: false},
		},
	}, handleStart)

	// xscroll_stop - Stop scrolling
	client.RegisterSkill(&pb.Skill{
		Name:        "xscroll_stop",
		Description: "Stop the current scrolling session",
		Parameters:  []*pb.SkillParameter{},
	}, handleStop)

	// xscroll_status - Get status
	client.RegisterSkill(&pb.Skill{
		Name:        "xscroll_status",
		Description: "Get the current status of the scrolling session",
		Parameters:  []*pb.SkillParameter{},
	}, handleStatus)

	// xscroll_get_tweets - Query stored tweets
	client.RegisterSkill(&pb.Skill{
		Name:        "xscroll_get_tweets",
		Description: "Query stored tweets from the database",
		Parameters: []*pb.SkillParameter{
			{Name: "limit", Type: "number", Description: "Maximum number of tweets to return (default: 50)", Required: false},
			{Name: "since_hours", Type: "number", Description: "Only return tweets from the last N hours", Required: false},
			{Name: "author", Type: "string", Description: "Filter by author handle (e.g., @elonmusk)", Required: false},
		},
	}, handleGetTweets)

	log.Println("Skills registered")
}

func handleStart(ctx context.Context, args map[string]string) (string, error) {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	if scrollRunning {
		return "", fmt.Errorf("scroll session already running (started at %s, %d tweets found)",
			scrollStats.StartedAt.Format(time.RFC3339), scrollStats.TweetsFound)
	}

	// Parse parameters
	debugPort := defaultDebugPort
	if v, ok := args["debug_port"]; ok && v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			debugPort = p
		}
	}

	scrollIntervalMs := defaultScrollIntervalMs
	if v, ok := args["scroll_interval_ms"]; ok && v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			scrollIntervalMs = i
		}
	}

	randomnessFactor := defaultRandomnessFactor
	if v, ok := args["randomness_factor"]; ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			randomnessFactor = f
		}
	}

	// Find X.com tab
	conn, err := findXcomTab(debugPort)
	if err != nil {
		return "", fmt.Errorf("failed to find X.com tab on port %d: %w", debugPort, err)
	}

	log.Printf("Found X.com tab: %s (%s)", conn.Tab.Title, conn.Tab.URL)

	// Create allocator context connected to the browser
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), conn.BrowserWSURL)

	// Create browser context attached to the existing X.com tab (don't create new tab)
	cdpCtx, cdpCancel := chromedp.NewContext(allocCtx,
		chromedp.WithTargetID(target.ID(conn.Tab.ID)),
	)

	// Initialize scroll session
	scrollCtx, scrollCancel = context.WithCancel(context.Background())
	scrollRunning = true
	scrollStats = ScrollStats{
		StartedAt:      time.Now(),
		TweetsFound:    0,
		ScrollCount:    0,
		DebugPort:      debugPort,
		ScrollInterval: scrollIntervalMs,
		Randomness:     randomnessFactor,
	}

	// Clear seen tweets for new session
	seenTweetIDsMu.Lock()
	seenTweetIDs = make(map[string]bool)
	seenTweetIDsMu.Unlock()

	// Start scrolling in background
	go func() {
		defer func() {
			allocCancel()
			cdpCancel()
			sessionMu.Lock()
			scrollRunning = false
			sessionMu.Unlock()
			log.Println("Scroll session ended")
		}()

		scrollLoop(cdpCtx, scrollIntervalMs, randomnessFactor)
	}()

	return fmt.Sprintf("Started scrolling session on X.com\n- Debug port: %d\n- Scroll interval: %dms\n- Randomness: %.0f%%\n\nUse xscroll_status to check progress, xscroll_stop to stop.",
		debugPort, scrollIntervalMs, randomnessFactor*100), nil
}

func handleStop(ctx context.Context, args map[string]string) (string, error) {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	if !scrollRunning {
		return "No scroll session is running", nil
	}

	stats := scrollStats
	stopScrolling()

	return fmt.Sprintf("Stopped scroll session\n- Duration: %s\n- Scrolls: %d\n- Tweets found: %d",
		time.Since(stats.StartedAt).Round(time.Second), stats.ScrollCount, stats.TweetsFound), nil
}

func stopScrolling() {
	if scrollCancel != nil {
		scrollCancel()
	}
	scrollRunning = false
}

func handleStatus(ctx context.Context, args map[string]string) (string, error) {
	sessionMu.Lock()
	running := scrollRunning
	stats := scrollStats
	sessionMu.Unlock()

	if !running {
		// Get total tweets in database
		rows, err := storage.Query(tweetsTable, nil, "", nil, "", 0, 0)
		if err != nil {
			return "No scroll session running. Failed to query database.", nil
		}
		return fmt.Sprintf("No scroll session running.\n\nDatabase contains %d tweets.", len(rows)), nil
	}

	duration := time.Since(stats.StartedAt).Round(time.Second)
	scrollRate := float64(stats.ScrollCount) / duration.Seconds() * 60 // scrolls per minute

	return fmt.Sprintf("Scroll session active\n- Started: %s (%s ago)\n- Debug port: %d\n- Scroll interval: %dms (±%.0f%%)\n- Scrolls: %d (%.1f/min)\n- Tweets found: %d",
		stats.StartedAt.Format(time.RFC3339),
		duration,
		stats.DebugPort,
		stats.ScrollInterval,
		stats.Randomness*100,
		stats.ScrollCount,
		scrollRate,
		stats.TweetsFound), nil
}

func handleGetTweets(ctx context.Context, args map[string]string) (string, error) {
	limit := int32(50)
	if v, ok := args["limit"]; ok && v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 {
			limit = int32(l)
		}
	}

	var whereClause string
	var whereArgs []string

	// Filter by time
	if v, ok := args["since_hours"]; ok && v != "" {
		if h, err := strconv.ParseFloat(v, 64); err == nil && h > 0 {
			sinceTime := time.Now().Add(-time.Duration(h * float64(time.Hour))).Unix()
			whereClause = "extracted_at > ?"
			whereArgs = append(whereArgs, strconv.FormatInt(sinceTime, 10))
		}
	}

	// Filter by author
	if v, ok := args["author"]; ok && v != "" {
		author := strings.TrimPrefix(v, "@")
		if whereClause != "" {
			whereClause += " AND author_handle LIKE ?"
		} else {
			whereClause = "author_handle LIKE ?"
		}
		whereArgs = append(whereArgs, "%"+author+"%")
	}

	rows, err := storage.Query(tweetsTable, nil, whereClause, whereArgs, "extracted_at DESC", limit, 0)
	if err != nil {
		return "", fmt.Errorf("failed to query tweets: %w", err)
	}

	if len(rows) == 0 {
		return "No tweets found matching the criteria", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d tweets:\n\n", len(rows)))

	for _, row := range rows {
		extractedAt, _ := strconv.ParseInt(row.Values["extracted_at"], 10, 64)
		t := time.Unix(extractedAt, 0)

		sb.WriteString(fmt.Sprintf("**@%s** (%s)\n", row.Values["author_handle"], row.Values["author_name"]))
		sb.WriteString(fmt.Sprintf("%s\n", truncate(row.Values["content"], 200)))
		sb.WriteString(fmt.Sprintf("_%s_ | %s\n\n", row.Values["timestamp"], t.Format("2006-01-02 15:04")))
	}

	return sb.String(), nil
}

// ChromeTarget represents a Chrome tab/target
type ChromeTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// ChromeConnection contains connection info
type ChromeConnection struct {
	Tab              *ChromeTarget
	BrowserWSURL     string
}

// findXcomTab finds a tab with X.com/Twitter open and returns connection info
func findXcomTab(port int) (*ChromeConnection, error) {
	// Get browser websocket URL
	versionResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Chrome: %w", err)
	}
	defer versionResp.Body.Close()

	versionBody, err := io.ReadAll(versionResp.Body)
	if err != nil {
		return nil, err
	}

	var versionData struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(versionBody, &versionData); err != nil {
		return nil, err
	}

	// Get list of tabs
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var targets []ChromeTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}

	// Find a tab with X.com or Twitter
	for _, t := range targets {
		if t.Type == "page" && (strings.Contains(t.URL, "x.com") || strings.Contains(t.URL, "twitter.com")) {
			return &ChromeConnection{
				Tab:          &t,
				BrowserWSURL: versionData.WebSocketDebuggerURL,
			}, nil
		}
	}

	// List available tabs for debugging
	var tabInfo []string
	for _, t := range targets {
		if t.Type == "page" {
			tabInfo = append(tabInfo, fmt.Sprintf("  - %s (%s)", t.Title, t.URL))
		}
	}

	if len(tabInfo) > 0 {
		return nil, fmt.Errorf("no X.com/Twitter tab found. Available tabs:\n%s", strings.Join(tabInfo, "\n"))
	}
	return nil, fmt.Errorf("no browser tabs found")
}

func scrollLoop(ctx context.Context, baseIntervalMs int, randomness float64) {
	for {
		select {
		case <-scrollCtx.Done():
			return
		default:
		}

		// Extract tweets before scrolling
		tweets, err := extractTweets(ctx)
		if err != nil {
			log.Printf("Failed to extract tweets: %v", err)
		} else {
			for _, tweet := range tweets {
				if err := saveTweet(tweet); err != nil {
					log.Printf("Failed to save tweet %s: %v", tweet.ID, err)
				} else {
					// Emit event for new tweet
					emitTweetEvent(tweet)

					sessionMu.Lock()
					scrollStats.TweetsFound++
					sessionMu.Unlock()
				}
			}
		}

		// Human-like scroll
		if err := humanScroll(ctx); err != nil {
			log.Printf("Failed to scroll: %v", err)
		}

		sessionMu.Lock()
		scrollStats.ScrollCount++
		sessionMu.Unlock()

		// Calculate next interval with randomness
		interval := float64(baseIntervalMs)
		variance := interval * randomness
		interval = interval - variance + rand.Float64()*variance*2

		// 10% chance of longer "reading" pause (2-5 seconds)
		if rand.Float64() < 0.1 {
			interval += float64(2000 + rand.Intn(3000))
		}

		select {
		case <-scrollCtx.Done():
			return
		case <-time.After(time.Duration(interval) * time.Millisecond):
		}
	}
}

func humanScroll(ctx context.Context) error {
	// Random scroll amount (300-700 pixels)
	scrollY := 300 + rand.Intn(400)

	// Random x/y position variance
	x := 400 + rand.Float64()*200 // Center-ish of screen
	y := 300 + rand.Float64()*200

	return chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseWheel, x, y).
				WithDeltaY(float64(scrollY)).
				Do(ctx)
		}),
	)
}

// JavaScript to extract tweets from the page
const extractTweetsJS = `
(function() {
    const tweets = [];
    const articles = document.querySelectorAll('article');

    articles.forEach(article => {
        try {
            // Skip already extracted tweets (marked with green background)
            if (article.dataset.xscrollExtracted === 'true') return;

            // Get tweet URL/ID from time element's parent link
            const timeEl = article.querySelector('time');
            if (!timeEl) return;

            const tweetLink = timeEl.closest('a');
            if (!tweetLink || !tweetLink.href) return;

            const url = tweetLink.href;
            const match = url.match(/\/status\/(\d+)/);
            if (!match) return;

            const id = match[1];

            // Get author info
            const userNameEl = article.querySelector('[data-testid="User-Name"]');
            let authorName = '';
            let authorHandle = '';

            if (userNameEl) {
                const spans = userNameEl.querySelectorAll('span');
                for (const span of spans) {
                    const text = span.textContent.trim();
                    if (text.startsWith('@')) {
                        authorHandle = text.substring(1);
                    } else if (text && !authorName && !text.includes('@') && text.length > 0) {
                        authorName = text;
                    }
                }
            }

            // Get tweet content
            const contentEl = article.querySelector('[data-testid="tweetText"]');
            const content = contentEl ? contentEl.textContent.trim() : '';

            // Get timestamp
            const timestamp = timeEl.getAttribute('datetime') || '';

            // Get images
            const images = [];
            article.querySelectorAll('img[src*="pbs.twimg.com/media"]').forEach(img => {
                if (img.src) images.push(img.src);
            });

            // Get videos
            const videos = [];
            article.querySelectorAll('video source, video[src]').forEach(v => {
                const src = v.src || v.getAttribute('src');
                if (src) videos.push(src);
            });

            // Mark as extracted with green background
            article.dataset.xscrollExtracted = 'true';
            article.style.backgroundColor = 'rgba(34, 197, 94, 0.15)';
            article.style.transition = 'background-color 0.3s ease';

            tweets.push({
                id: id,
                url: url,
                author_name: authorName,
                author_handle: authorHandle,
                content: content,
                timestamp: timestamp,
                images: images,
                videos: videos
            });
        } catch (e) {
            console.error('Error extracting tweet:', e);
        }
    });

    return tweets;
})()
`

func extractTweets(ctx context.Context) ([]Tweet, error) {
	var result []interface{}
	if err := chromedp.Run(ctx, chromedp.Evaluate(extractTweetsJS, &result)); err != nil {
		return nil, err
	}

	var tweets []Tweet
	now := time.Now().Unix()

	for _, item := range result {
		data, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		id, _ := data["id"].(string)
		if id == "" {
			continue
		}

		// Skip if already seen in this session
		seenTweetIDsMu.RLock()
		seen := seenTweetIDs[id]
		seenTweetIDsMu.RUnlock()
		if seen {
			continue
		}

		// Mark as seen
		seenTweetIDsMu.Lock()
		seenTweetIDs[id] = true
		seenTweetIDsMu.Unlock()

		tweet := Tweet{
			ID:           id,
			URL:          getString(data, "url"),
			AuthorName:   getString(data, "author_name"),
			AuthorHandle: getString(data, "author_handle"),
			Content:      getString(data, "content"),
			Timestamp:    getString(data, "timestamp"),
			Images:       getStringSlice(data, "images"),
			Videos:       getStringSlice(data, "videos"),
			ExtractedAt:  now,
		}

		tweets = append(tweets, tweet)
	}

	return tweets, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return sanitizeUTF8(v)
	}
	return ""
}

func getStringSlice(m map[string]interface{}, key string) []string {
	var result []string
	if v, ok := m[key].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, sanitizeUTF8(s))
			}
		}
	}
	return result
}

func saveTweet(tweet Tweet) error {
	// Check if tweet already exists in database
	rows, err := storage.Query(tweetsTable, []string{"id"}, "id = ?", []string{tweet.ID}, "", 1, 0)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		// Already exists, skip
		return nil
	}

	imagesJSON, _ := json.Marshal(tweet.Images)
	videosJSON, _ := json.Marshal(tweet.Videos)

	return storage.Insert(tweetsTable, map[string]string{
		"id":            tweet.ID,
		"url":           tweet.URL,
		"author_name":   tweet.AuthorName,
		"author_handle": tweet.AuthorHandle,
		"content":       tweet.Content,
		"timestamp":     tweet.Timestamp,
		"images":        string(imagesJSON),
		"videos":        string(videosJSON),
		"extracted_at":  strconv.FormatInt(tweet.ExtractedAt, 10),
	})
}

func emitTweetEvent(tweet Tweet) {
	// Convert tweet to map for structpb
	payload, err := structpb.NewStruct(map[string]interface{}{
		"id":            tweet.ID,
		"url":           tweet.URL,
		"author_name":   tweet.AuthorName,
		"author_handle": tweet.AuthorHandle,
		"content":       tweet.Content,
		"timestamp":     tweet.Timestamp,
		"images":        toInterfaceSlice(tweet.Images),
		"videos":        toInterfaceSlice(tweet.Videos),
		"extracted_at":  tweet.ExtractedAt,
	})
	if err != nil {
		log.Printf("Failed to create event payload: %v", err)
		return
	}

	event := &pb.Event{
		EventType: "xscroll.tweet.extracted",
		Timestamp: timestamppb.Now(),
		Data: &pb.Event_Generic{
			Generic: &pb.GenericEvent{
				EventName: "tweet_extracted",
				Payload:   payload,
			},
		},
	}

	if err := client.Emit(event); err != nil {
		log.Printf("Failed to emit event: %v", err)
	}
}

func toInterfaceSlice(s []string) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
}

func cleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Run immediately on startup
	cleanupOldTweets()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupOldTweets()
		}
	}
}

func cleanupOldTweets() {
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := storage.Delete(tweetsTable, "extracted_at < ?", strconv.FormatInt(cutoff, 10)); err != nil {
		log.Printf("Failed to cleanup old tweets: %v", err)
	} else {
		log.Printf("Cleaned up tweets older than %d days", retentionDays)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// sanitizeUTF8 removes invalid UTF-8 sequences from a string
func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	// Replace invalid sequences with replacement character
	return strings.ToValidUTF8(s, "\uFFFD")
}
