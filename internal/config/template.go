package config

// Template returns a commented starter config.toml.
func Template() string {
	return `# social-mcp configuration. Platform sign-in needs no setup: run "social-mcp account add <platform>".

[uploads]
# Optional: restrict uploads to these absolute directories. Empty allows any path.
# allowed_dirs = ["/home/me/Videos"]
allowed_dirs = []
max_concurrent = 2

# The daemon checks GitHub once a day for a new social-mcp release and mentions it once in the CLI and to
# your agent. To turn the check off, uncomment these two lines (or set SOCIAL_MCP_NO_UPDATE_CHECK=1);
# "social-mcp update" still works. Left commented out, this file also stays readable by older releases.
# [updates]
# check = false
`
}
