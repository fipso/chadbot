# MQTT Plugin

Subscribe to MQTT topics and receive messages. Publish messages to control devices.

## Important: Always Check Subscriptions First

**Before doing anything MQTT-related, ALWAYS call `mqtt_status` or `mqtt_list_subscriptions` first** to see what topics are currently being monitored. This gives you context about:
- What data sources are available
- The naming patterns used (e.g., `device/sensor/type/state`)
- Whether the broker is connected

## Topic Inference

When the user asks about data without specifying exact topics:
- Use the subscribed topics list to infer which topic they mean
- Match partial names, sensor types, or device names
- Example: "what's the temperature?" → find topic containing "temperature"
- Example: "humidity in the box" → find topic containing "box" and "humidity"

## Skills

### mqtt_status
Get connection status and list of active topics. **Call this first.**

### mqtt_list_subscriptions
List all subscribed topics with details. Use to understand available data sources.

### mqtt_get_messages
Get recent messages from a specific topic.
- `topic`: Exact topic path (use subscriptions list to find correct path)
- `limit`: Number of messages (default: 10)

### mqtt_subscribe
Subscribe to a new topic.
- `topic`: Topic pattern (supports `+` single-level and `#` multi-level wildcards)
- `qos`: Quality of Service 0, 1, or 2

### mqtt_unsubscribe
Remove a subscription.

### mqtt_publish
Send a message to a topic.
- `topic`: Target topic
- `payload`: Message content
- `qos`: Quality of Service
- `retain`: Keep message on broker for new subscribers

### mqtt_reconnect
Reconnect to the broker if disconnected.

## Common Patterns

**Getting sensor data:**
1. Call `mqtt_status` to see active topics
2. Identify the relevant topic from the list
3. Call `mqtt_get_messages` with that exact topic

**Controlling a device:**
1. Check existing topics to understand the naming scheme
2. Typically command topics use `/set` or `/command` suffix
3. Publish with appropriate payload format (often JSON or plain values)

## Topic Naming Conventions

Common patterns you might see:
- `device/sensor/type/state` - sensor readings
- `device/switch/name/state` - switch states
- `device/switch/name/set` - switch commands
- `homeassistant/sensor/...` - Home Assistant discovery
- `zigbee2mqtt/device/...` - Zigbee2MQTT devices
