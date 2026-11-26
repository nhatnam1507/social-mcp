package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nhatnam1507/social-mcp/internal/paths"
	"github.com/nhatnam1507/social-mcp/internal/wsl"
)

// serverName is the key social-mcp is registered under in an agent's MCP configuration.
const serverName = "social-mcp"

// stdioServer is the MCP server entry an agent uses to launch `social-mcp relay`.
type stdioServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// localServer launches the binary directly, for an agent on the same machine.
func localServer(exe string) stdioServer {
	s := stdioServer{Command: exe, Args: []string{"relay"}}
	if h := os.Getenv(paths.EnvHome); h != "" {
		s.Env = map[string]string{paths.EnvHome: h}
	}
	return s
}

// windowsServer launches the binary inside WSL through wsl.exe, for a Windows app while social-mcp
// lives in WSL. Environment variables do not cross wsl.exe, so a custom home is passed through `env`.
func windowsServer(exe string) stdioServer {
	var args []string
	if d := wsl.Distro(); d != "" {
		args = append(args, "-d", d)
	}
	args = append(args, "--")
	if h := os.Getenv(paths.EnvHome); h != "" {
		args = append(args, "env", paths.EnvHome+"="+h)
	}
	return stdioServer{Command: "wsl.exe", Args: append(args, exe, "relay")}
}

// app is an AI app that `connect` sets up.
type app struct {
	name, title string
	// desktop apps run on Windows when social-mcp runs in WSL: their settings live in the Windows user
	// profile and they reach social-mcp through wsl.exe. Terminal tools run next to social-mcp.
	desktop bool
	find    func() (string, error)                                                // the settings file
	write   func(path string, s stdioServer, now time.Time) (string, bool, error) // backup, replaced
	connect func(out io.Writer, exe string) error                                 // apps set up through their own CLI
	next    string                                                                // what to do afterwards
}

var apps = []app{
	{
		name: "claude-desktop", title: "Claude Desktop", desktop: true, find: findClaudeDesktop, write: addServer,
		next: "Now quit Claude Desktop completely (also from the system tray or menu bar) and open it again.\n" +
			"Ask Claude to list your social accounts to try it.",
	},
	{
		name: "claude-code", title: "Claude Code", connect: connectClaudeCode,
	},
	{
		name: "cursor", title: "Cursor", desktop: true, find: findCursor, write: addServer,
		next: "Restart Cursor, then check under Cursor Settings > MCP that social-mcp is turned on.",
	},
	{
		name: "codex", title: "Codex", find: findCodex, write: setCodexServer,
		next: "Start a new Codex session; `/mcp` lists social-mcp.",
	},
	{
		name: "antigravity", title: "Antigravity", desktop: true, find: findAntigravity, write: addServer,
		next: "Restart Antigravity (or open MCP Servers > Manage MCP Servers in the agent panel and refresh);\n" +
			"social-mcp then shows in the list.",
	},
}

func appNames() []string {
	names := make([]string, len(apps))
	for i, a := range apps {
		names[i] = a.name
	}
	return names
}

func connectCmd() *cobra.Command {
	var configFile string
	var rotateToken bool
	cmd := &cobra.Command{
		Use:   "connect [" + strings.Join(appNames(), "|") + "]",
		Short: "Connect social-mcp to your AI app (one time)",
		Long: "Connects social-mcp to an AI app. Do it once per app: the app then starts social-mcp by itself\n" +
			"(and the daemon, when it is not running). Settings files are edited in place: your other servers\n" +
			"stay, and the old file is saved as a backup next to it.\n\n" +
			"  social-mcp connect claude-desktop   Claude Desktop\n" +
			"  social-mcp connect claude-code      Claude Code (terminal)\n" +
			"  social-mcp connect cursor           Cursor\n" +
			"  social-mcp connect codex            Codex (terminal and IDE extension)\n" +
			"  social-mcp connect antigravity      Antigravity\n" +
			"  social-mcp connect                  the settings to paste into any other AI app\n\n" +
			"Inside WSL, the desktop apps (Claude Desktop, Cursor, Antigravity) are set up on the Windows side\n" +
			"and reach social-mcp through wsl.exe; Claude Code and Codex are set up inside WSL.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: appNames(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return showSetup(cmd, rotateToken)
			}
			for _, a := range apps {
				if a.name == args[0] {
					exe, err := os.Executable()
					if err != nil {
						return err
					}
					return connectApp(cmd.OutOrStdout(), a, exe, configFile)
				}
			}
			return fmt.Errorf("unknown app %q: choose one of %s, or run `social-mcp connect` for the settings to paste into other apps",
				args[0], strings.Join(appNames(), ", "))
		},
	}
	cmd.Flags().StringVar(&configFile, "config-file", "", "the app's settings file, when it is not found automatically")
	cmd.Flags().BoolVar(&rotateToken, "rotate-token", false, "with no app: replace the HTTP token (restart the daemon afterwards)")
	return cmd
}

func connectApp(out io.Writer, a app, exe, configFile string) error {
	if a.connect != nil {
		if configFile != "" {
			return fmt.Errorf("--config-file does not apply to %s", a.name)
		}
		return a.connect(out, exe)
	}
	bridged := a.desktop && wsl.Detected()
	server := localServer(exe)
	if bridged {
		server = windowsServer(exe)
	}
	path := configFile
	if path == "" {
		var err error
		if path, err = a.find(); err != nil {
			return err
		}
	}
	backup, replaced, err := a.write(path, server, time.Now())
	if err != nil {
		return err
	}
	verb := "added social-mcp to"
	if replaced {
		verb = "replaced the social-mcp entry in"
	}
	fmt.Fprintf(out, "%s %s settings: %s\n", verb, a.title, path)
	if backup != "" {
		fmt.Fprintf(out, "backup of the previous file: %s\n", backup)
	}
	if bridged {
		fmt.Fprintf(out, "%s on Windows will start social-mcp inside WSL (%s).\n", a.title, strings.Join(append([]string{server.Command}, server.Args...), " "))
	}
	fmt.Fprintln(out, "\n"+a.next)
	return nil
}

func connectClaudeCode(out io.Writer, exe string) error {
	claude, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("the `claude` command was not found; install Claude Code, or use `social-mcp connect claude-desktop` for the desktop app")
	}
	replaced := false
	if exec.Command(claude, "mcp", "get", serverName).Run() == nil {
		if b, err := exec.Command(claude, "mcp", "remove", "--scope", "user", serverName).CombinedOutput(); err != nil {
			return fmt.Errorf("social-mcp is already registered in Claude Code outside your user settings; remove it with `claude mcp remove %s` and run this again (%s)", serverName, strings.TrimSpace(string(b)))
		}
		replaced = true
	}
	if b, err := exec.Command(claude, "mcp", "add", "--scope", "user", serverName, "--", exe, "relay").CombinedOutput(); err != nil {
		return fmt.Errorf("claude mcp add failed: %s", strings.TrimSpace(string(b)))
	}
	if replaced {
		fmt.Fprintln(out, "replaced the earlier social-mcp registration in Claude Code (user scope)")
	} else {
		fmt.Fprintln(out, "registered social-mcp in Claude Code (user scope, all projects)")
	}
	fmt.Fprintln(out, "New Claude Code sessions connect by themselves; in a session that is already open, run /mcp and reconnect social-mcp.")
	return nil
}

// desktopHome returns the user's home folder as desktop apps see it: the Windows profile inside WSL.
func desktopHome() (string, error) {
	if wsl.Detected() {
		h, err := wsl.WindowsEnvDir("USERPROFILE")
		if err != nil {
			return "", fmt.Errorf("cannot find your Windows user folder from WSL: %w; pass the file with --config-file", err)
		}
		return h, nil
	}
	return os.UserHomeDir()
}

func findClaudeDesktop() (string, error) {
	const name = "claude_desktop_config.json"
	notFound := errors.New("Claude Desktop's settings file was not found. Open Claude Desktop, go to Settings > Developer > " +
		"Edit Config (this creates the file), then run this again; or pass the file with --config-file")
	switch {
	case runtime.GOOS == "darwin":
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return pickConfig([]string{filepath.Join(h, "Library", "Application Support", "Claude")}, name, notFound)
	case runtime.GOOS == "windows":
		return pickWindowsConfig(os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"), name, notFound)
	case wsl.Detected():
		local, err := wsl.WindowsEnvDir("LOCALAPPDATA")
		if err != nil {
			return "", fmt.Errorf("cannot find your Windows user folders from WSL: %w; pass the file with --config-file", err)
		}
		roaming, err := wsl.WindowsEnvDir("APPDATA")
		if err != nil {
			return "", fmt.Errorf("cannot find your Windows user folders from WSL: %w; pass the file with --config-file", err)
		}
		return pickWindowsConfig(local, roaming, name, notFound)
	default:
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return pickConfig([]string{filepath.Join(dir, "Claude")}, name, notFound)
	}
}

// pickWindowsConfig covers the places Claude Desktop versions on Windows keep their settings: the
// per-package data folder, the packaged app's redirected AppData, and classic %APPDATA%\Claude.
func pickWindowsConfig(localAppData, appData, name string, notFound error) (string, error) {
	if localAppData == "" && appData == "" {
		return "", notFound
	}
	var dirs []string
	if localAppData != "" {
		data, _ := filepath.Glob(filepath.Join(localAppData, "Claude-Data", "*"))
		pkg, _ := filepath.Glob(filepath.Join(localAppData, "Packages", "Claude_*", "LocalCache", "Roaming", "Claude"))
		sort.Strings(data)
		sort.Strings(pkg)
		dirs = append(append(dirs, data...), pkg...)
	}
	if appData != "" {
		dirs = append(dirs, filepath.Join(appData, "Claude"))
	}
	return pickConfig(dirs, name, notFound)
}

func findCursor() (string, error) {
	h, err := desktopHome()
	if err != nil {
		return "", err
	}
	return pickConfig([]string{filepath.Join(h, ".cursor")}, "mcp.json",
		errors.New("Cursor was not found (no .cursor folder in your home folder). Open Cursor once, then run this again; or pass the file with --config-file"))
}

func findCodex() (string, error) {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(h, ".codex")
	}
	return pickConfig([]string{dir}, "config.toml",
		fmt.Errorf("Codex was not found (no %s folder). Run Codex once, then run this again; or pass the file with --config-file", dir))
}

// findAntigravity returns Antigravity's global MCP settings, ~/.gemini/config/mcp_config.json. Early
// versions read ~/.gemini/antigravity/mcp_config.json, which is used only while it is the file that exists.
func findAntigravity() (string, error) {
	h, err := desktopHome()
	if err != nil {
		return "", err
	}
	return antigravityConfig(h)
}

func antigravityConfig(h string) (string, error) {
	const name = "mcp_config.json"
	current := filepath.Join(h, ".gemini", "config")
	legacy := filepath.Join(h, ".gemini", "antigravity")
	if p, err := pickConfig([]string{current, legacy}, name, nil); err == nil && fileExists(p) {
		return p, nil
	}
	for _, d := range []string{current, legacy} {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return filepath.Join(current, name), nil
		}
	}
	return "", errors.New("Antigravity was not found (no .gemini/config or .gemini/antigravity folder in your home folder). " +
		"Open Antigravity once, then run this again; or pass the file with --config-file")
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// pickConfig returns the most recently written settings file named name in dirs. When none exists
// yet, it returns the path in the first of dirs that exists as a folder.
func pickConfig(dirs []string, name string, notFound error) (string, error) {
	var best string
	var bestTime time.Time
	for _, d := range dirs {
		fi, err := os.Stat(filepath.Join(d, name))
		if err == nil && fi.Mode().IsRegular() && (best == "" || fi.ModTime().After(bestTime)) {
			best, bestTime = filepath.Join(d, name), fi.ModTime()
		}
	}
	if best != "" {
		return best, nil
	}
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return filepath.Join(d, name), nil
		}
	}
	return "", notFound
}
