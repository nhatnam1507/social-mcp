package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// addServer sets mcpServers["social-mcp"] in a JSON settings file (Claude Desktop, Cursor,
// Antigravity), keeping every other setting and the order of keys. The file is left untouched when it
// is not valid JSON.
func addServer(path string, server stdioServer, now time.Time) (backup string, replaced bool, err error) {
	orig, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	root, err := parseObject(orig)
	if err != nil {
		return "", false, fmt.Errorf("%s is not valid JSON (%v); fix it in a text editor and run this again", path, err)
	}
	var servers object
	if raw, ok := root.get("mcpServers"); ok {
		if servers, err = parseObject(raw); err != nil {
			return "", false, fmt.Errorf("%s: mcpServers is not a JSON object", path)
		}
	}
	_, replaced = servers.get(serverName)
	entry, err := json.Marshal(server)
	if err != nil {
		return "", false, err
	}
	servers.set(serverName, entry)
	root.set("mcpServers", servers.marshal())
	var buf bytes.Buffer
	if err := json.Indent(&buf, root.marshal(), "", "  "); err != nil {
		return "", false, err
	}
	buf.WriteByte('\n')
	backup, err = replaceFile(path, orig, buf.Bytes(), now)
	return backup, replaced, err
}

// tomlHeader matches a TOML table header line such as `[mcp_servers.social-mcp]` or `[[x]]`.
var tomlHeader = regexp.MustCompile(`^\s*\[\[?\s*([^\[\]]+?)\s*\]\]?\s*(#.*)?$`)

// setCodexServer sets the [mcp_servers.social-mcp] table in Codex's config.toml. The rest of the file,
// comments included, stays as it is; an earlier social-mcp table and its sub-tables are replaced.
func setCodexServer(path string, server stdioServer, now time.Time) (backup string, replaced bool, err error) {
	orig, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	var before map[string]any
	if _, err := toml.Decode(string(orig), &before); err != nil {
		return "", false, fmt.Errorf("%s is not valid TOML (%v); fix it in a text editor and run this again", path, err)
	}
	_, replaced = codexServer(before)

	var kept []string
	skipping := false
	for _, line := range strings.Split(string(orig), "\n") {
		if m := tomlHeader.FindStringSubmatch(line); m != nil {
			key := tomlKey(m[1])
			skipping = key == "mcp_servers."+serverName || strings.HasPrefix(key, "mcp_servers."+serverName+".")
		}
		if !skipping {
			kept = append(kept, line)
		}
	}
	var b strings.Builder
	if s := strings.TrimRight(strings.Join(kept, "\n"), " \t\r\n"); s != "" {
		b.WriteString(s + "\n\n")
	}
	fmt.Fprintf(&b, "[mcp_servers.%s]\ncommand = %s\nargs = [", serverName, tomlString(server.Command))
	for i, a := range server.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tomlString(a))
	}
	b.WriteString("]\n")
	if len(server.Env) > 0 {
		var pairs []string
		for k, v := range server.Env {
			pairs = append(pairs, k+" = "+tomlString(v))
		}
		b.WriteString("env = { " + strings.Join(pairs, ", ") + " }\n")
	}

	var after map[string]any
	_, derr := toml.Decode(b.String(), &after)
	got, ok := codexServer(after)
	if derr != nil || !ok || got["command"] != server.Command {
		return "", false, fmt.Errorf("%s defines social-mcp in a way this command cannot update; remove the social-mcp entry by hand and run this again", path)
	}
	backup, err = replaceFile(path, orig, []byte(b.String()), now)
	return backup, replaced, err
}

func codexServer(cfg map[string]any) (map[string]any, bool) {
	servers, _ := cfg["mcp_servers"].(map[string]any)
	s, ok := servers[serverName].(map[string]any)
	return s, ok
}

// tomlKey normalizes a dotted TOML key: `mcp_servers . "social-mcp"` becomes `mcp_servers.social-mcp`.
func tomlKey(k string) string {
	parts := strings.Split(k, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, ".")
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// replaceFile copies a non-empty orig to a timestamped backup next to path, then replaces path with
// data atomically.
func replaceFile(path string, orig, data []byte, now time.Time) (backup string, err error) {
	if len(orig) > 0 {
		backup = fmt.Sprintf("%s.%s.bak", path, now.Format("20060102-150405"))
		if err := os.WriteFile(backup, orig, 0o600); err != nil {
			return "", fmt.Errorf("back up %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return "", err
	}
	_, werr := tmp.Write(data)
	if err := errors.Join(werr, tmp.Close()); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return backup, nil
}

// object is a JSON object that keeps its keys in their original order.
type object struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseObject(data []byte) (object, error) {
	o := object{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return o, errors.New("expected a JSON object")
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return o, err
		}
		key, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return o, err
		}
		o.set(key, v)
	}
	if _, err := dec.Token(); err != nil {
		return o, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return o, errors.New("unexpected data after the JSON object")
	}
	return o, nil
}

func (o *object) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

func (o *object) set(key string, v json.RawMessage) {
	if o.vals == nil {
		o.vals = map[string]json.RawMessage{}
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

func (o object) marshal() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}
