# XScroll Plugin

Scroll through X.com (Twitter) with human-like behavior and extract tweets as events.

## Prerequisites

1. Start Chrome with remote debugging enabled:
   ```bash
   google-chrome --remote-debugging-port=9222
   # or for Chromium:
   chromium --remote-debugging-port=9222
   ```

2. Navigate to X.com/Twitter and log in

3. Navigate to the timeline or feed you want to scroll through

## Skills

### xscroll_start

Start scrolling and extracting tweets from X.com.

**Parameters:**
- `debug_port` (optional): Chrome debug port (default: 9222)
- `scroll_interval_ms` (optional): Base interval between scrolls in milliseconds (default: 2000)
- `randomness_factor` (optional): Randomness factor 0-1 for scroll timing variance (default: 0.3)

**Example:**
```
xscroll_start debug_port=9222 scroll_interval_ms=3000 randomness_factor=0.4
```

### xscroll_stop

Stop the current scrolling session.

### xscroll_status

Get the current status of the scrolling session, including:
- Duration
- Number of scrolls performed
- Number of tweets extracted
- Configuration values

### xscroll_get_tweets

Query stored tweets from the database.

**Parameters:**
- `limit` (optional): Maximum number of tweets to return (default: 50)
- `since_hours` (optional): Only return tweets from the last N hours
- `author` (optional): Filter by author handle (e.g., @elonmusk or elonmusk)

**Example:**
```
xscroll_get_tweets limit=20 since_hours=24 author=elonmusk
```

## Events

### xscroll.tweet.extracted

Emitted for each new tweet found. Payload contains:
- `id` - Tweet ID
- `url` - Full tweet URL
- `author_name` - Display name of the author
- `author_handle` - Twitter handle (without @)
- `content` - Tweet text content
- `timestamp` - ISO timestamp from Twitter
- `images` - Array of image URLs
- `videos` - Array of video URLs
- `extracted_at` - Unix timestamp when extracted

## Human-Like Behavior

The plugin scrolls with human-like patterns:
- Random scroll amounts (300-700 pixels)
- Random screen position for scroll events
- Base interval with configurable randomness (default 30% variance)
- 10% chance of longer "reading" pauses (2-5 seconds)

## Data Retention

- Tweets are stored in a SQLite database
- Automatic cleanup of tweets older than 7 days
- Cleanup runs hourly

## Tips

- Use a slow scroll interval (3000-5000ms) to avoid rate limiting
- Higher randomness (0.4-0.5) makes scrolling look more natural
- Subscribe to `xscroll.tweet.extracted` events from other plugins for real-time processing
- Use `xscroll_get_tweets` with filters to query historical data

## Troubleshooting

**"Failed to connect to Chrome"**
- Ensure Chrome is running with `--remote-debugging-port=9222`
- Check if the port is correct
- Try `curl http://127.0.0.1:9222/json/version` to verify

**"Current tab is not X.com/Twitter"**
- Navigate to x.com or twitter.com before starting
- The plugin checks the URL of the active tab

**No tweets being extracted**
- Ensure you're logged in to X.com
- Check if the page has loaded completely
- Try scrolling manually to verify content loads
