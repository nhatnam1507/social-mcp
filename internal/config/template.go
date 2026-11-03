package config

// Template returns a commented starter config.toml.
func Template() string {
	return `# social-mcp configuration. See docs/platform-setup.md for registering each app.

[server]
# Loopback port for the MCP endpoint (http://127.0.0.1:<port>/mcp) and OAuth callbacks.
# Register http://127.0.0.1:<port>/oauth/callback (Google, TikTok) and
# http://localhost:<port>/oauth/callback (Meta) as redirect URIs.
listen_port = 8765

[uploads]
# Optional: restrict uploads to these absolute directories. Empty allows any path.
# allowed_dirs = ["/home/me/Videos"]
allowed_dirs = []
max_concurrent = 2

[youtube]
# Google Cloud OAuth client of type "Desktop app" with the YouTube Data API v3 enabled.
client_id = ""
client_secret = ""

[tiktok]
# TikTok developer app with Login Kit (Desktop) and the Content Posting API.
client_key = ""
client_secret = ""

[meta]
# Meta Business app with Facebook Login for Business. login_config_id is the
# configuration ID that grants the Page and Instagram permissions.
app_id = ""
app_secret = ""
login_config_id = ""
graph_version = "v26.0"
# "development" (posts visible only to app roles) or "live".
app_mode = "development"
`
}
