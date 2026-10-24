package unit

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// IsTemplate reports whether a unit name has an empty instance.
func IsTemplate(name string) bool {
	return strings.HasSuffix(strings.TrimSuffix(name, filepath.Ext(name)), "@")
}

var dependencyKeys = map[string]bool{
	"Requires": true, "Requisite": true, "Wants": true, "BindsTo": true, "Upholds": true,
	"PartOf": true, "OnFailure": true, "OnSuccess": true, "Before": true, "After": true,
	"PropagatesStopTo": true, "PropagatesReloadTo": true, "JoinsNamespaceOf": true,
	"Unit": true, "Service": true, "Sockets": true, "Slice": true,
	"WantedBy": true, "RequiredBy": true, "UpheldBy": true, "Also": true, "Alias": true,
}

// Conservatively match specifiers against the candidate names. This also covers
// templates and installation/ordering references. A reset may cause
// a false positive; deletion must never ignore an ambiguous reference.
func References(content string, names []string) bool {
	section := ""
	pending := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		line = pending + line
		pending = ""
		if strings.HasSuffix(line, "\\") {
			pending = strings.TrimSuffix(line, "\\") + " "
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[Unit]" && section != "[Service]" && section != "[Socket]" && section != "[Timer]" && section != "[Path]" && section != "[Install]" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if strings.TrimSpace(key) == "RequiresMountsFor" || strings.TrimSpace(key) == "WantsMountsFor" {
			for _, name := range names {
				if strings.HasSuffix(name, ".mount") && strings.TrimSpace(value) != "" {
					return true
				}
			}
		}
		if !ok || !dependencyKeys[strings.TrimSpace(key)] {
			continue
		}
		for _, token := range strings.Fields(value) {
			token = strings.NewReplacer("\"", "", "'", "").Replace(token)
			pattern := "^"
			for i := 0; i < len(token); i++ {
				if token[i] == '\\' {
					if i+3 < len(token) && token[i+1] == 'x' {
						if value, err := strconv.ParseUint(token[i+2:i+4], 16, 8); err == nil {
							pattern += "(?:" + regexp.QuoteMeta(token[i:i+4]) + "|" + regexp.QuoteMeta(string(rune(value))) + ")"
							i += 3
							continue
						}
					}
					// Unknown escapes remain conservative without matching unrelated names.
					pattern += ".*"
					if i+1 < len(token) {
						i++
					}
					continue
				}
				if token[i] == '%' && i+1 < len(token) {
					i++
					if token[i] == '%' {
						pattern += "%"
					} else {
						pattern += ".*"
					}
					continue
				}
				pattern += regexp.QuoteMeta(string(token[i]))
			}
			pattern += "$"
			for _, name := range names {
				if ReferencesName(token, name) {
					return true
				}
				if strings.Contains(token, "\\") {
					if decoded, err := strconv.Unquote("\"" + token + "\""); err == nil && ReferencesName(decoded, name) {
						return true
					}
				}
				if ok, _ := regexp.MatchString(pattern, name); ok {
					return true
				}
			}
		}
	}
	return pending != ""
}

// ReferencesName also matches instances when the target is a template.
func ReferencesName(value, name string) bool {
	if value == name {
		return true
	}
	if IsTemplate(name) {
		prefix, suffix, _ := strings.Cut(name, "@.")
		return strings.HasPrefix(value, prefix+"@") && strings.HasSuffix(value, "."+suffix)
	}
	return false
}

// ImplicitReferences checks activation defaults for timers, sockets and paths.
func ImplicitReferences(name string, names []string) bool {
	ext := filepath.Ext(name)
	if ext != ".timer" && ext != ".socket" && ext != ".path" {
		return false
	}
	base := strings.TrimSuffix(name, ext)
	implied := base + ".service"
	if IsTemplate(name) {
		implied = strings.TrimSuffix(base, "@") + "@%i.service"
	}
	// Accept=yes sockets can activate instances without an explicit Service=.
	if ext == ".socket" && !strings.Contains(base, "@") {
		implied += " " + base + "@%i.service"
	}
	return References("[Timer]\nUnit="+implied, names)
}
