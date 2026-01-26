# MCP Plugin

This plugin integrates Model Context Protocol (MCP) servers, allowing you to spawn and interact with external tools.

## Workflow

1. **Add a server** with `mcp_add_server` (persisted to storage)
2. **Connect** with `mcp_connect` to spawn the subprocess
3. **List tools** with `mcp_list_tools` to see available capabilities
4. **Call tools** with `mcp_call_tool` to execute actions
5. **Disconnect** with `mcp_disconnect` when done

## Important Guidelines

### Iterative Tool Calls Are Expected

Many MCP tools interact with dynamic environments (browsers, APIs, file systems) where the exact state isn't known upfront. **It's completely normal and expected to retry tool calls until they succeed.**

Common scenarios where retrying is appropriate:
- **Browser automation**: When evaluating JavaScript without knowing the DOM structure, try different selectors or approaches until you find what works
- **File operations**: When searching for files or content, iterate with different paths or patterns
- **API interactions**: When the response format varies, adjust your approach based on what you receive

Don't hesitate to:
- Call a tool, observe the result, and call it again with adjusted parameters
- Try multiple selector strategies when interacting with web pages
- Explore the environment incrementally rather than trying to get it perfect on the first attempt

### Tool Arguments

The `mcp_call_tool` skill expects a `arguments` parameter as a JSON string. Always format it as a valid JSON object:
```
arguments: {"uid": "1_10", "value": "some text"}
```

### Browser Automation (chrome-devtools)

When using browser automation tools like `fill`, `click`, `type`, or `select`:
- **Use `uid` NOT CSS selectors** - Tools require the element's `uid` from the page snapshot (e.g., `uid=1_10`), not CSS selectors like `input[name='foo']`
- The page snapshot returned by tools shows elements with their uids: `uid=1_10 combobox "Search for location"`
- Extract the uid number and pass it: `{"uid": "1_10", "value": "my input"}`
- If you get errors about missing `uid`, you're probably trying to use a CSS selector instead

**Tool differences:**
- `type` - Types text character by character into an input field (use for free text entry)
- `fill` - Selects an option from a dropdown/combobox (use for select elements with predefined options)
- `click` - Clicks on an element
- `press_key` - Presses a keyboard key (e.g., "Enter", "Tab", "Escape")

For autocomplete fields: use `type` to enter text, wait for suggestions, then `click` on a suggestion or `press_key` with "Enter"

### Server Configuration

When adding servers with `mcp_add_server`:
- `args` should be a JSON array: `["--port", "3000"]`
- `env` should be a JSON object: `{"API_KEY": "xxx"}`
- Set `auto_start: true` for servers you want connected automatically on plugin startup

## Available Skills

| Skill | Purpose |
|-------|---------|
| `mcp_add_server` | Register a new MCP server configuration |
| `mcp_remove_server` | Remove a server configuration |
| `mcp_connect` | Spawn and connect to a server |
| `mcp_disconnect` | Terminate a server connection |
| `mcp_list_servers` | List all configured servers with status |
| `mcp_list_tools` | List tools from connected servers |
| `mcp_call_tool` | Execute a tool on a connected server |
| `mcp_status` | Get plugin status overview |
