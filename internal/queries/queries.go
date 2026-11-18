// Package queries stores named SQL queries as <name>.sql files in a
// directory (by default "saved_queries" next to the config file, e.g.
// ~/.config/trackz/saved_queries on Linux).
package queries

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Ext is the file extension of saved queries.
const Ext = ".sql"

// ErrExists is returned by Save when a query of that name exists and
// overwriting was not requested.
var ErrExists = errors.New("a saved query with this name already exists")

// Store manages the queries in one directory.
type Store struct{ Dir string }

// Query is a saved query.
type Query struct {
	Name     string
	Path     string
	Modified time.Time
}

// ValidateName rejects names that are empty, too long, hidden, or not
// usable as a file name on every supported OS.
func ValidateName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("the name must not be empty")
	case name != strings.TrimSpace(name):
		return errors.New("the name must not start or end with spaces")
	case len([]rune(name)) > 100:
		return errors.New("the name must be at most 100 characters")
	case strings.HasPrefix(name, "."):
		return errors.New("the name must not start with a dot")
	case strings.HasSuffix(name, "."):
		return errors.New("the name must not end with a dot")
	}
	for _, r := range name {
		if strings.ContainsRune(`/\:*?"<>|`, r) || unicode.IsControl(r) {
			return fmt.Errorf("the name must not contain %q", r)
		}
	}
	return nil
}

// path returns the file for name; the name must be valid.
func (s Store) path(name string) string { return filepath.Join(s.Dir, name+Ext) }

// List returns the saved queries sorted by name (case-insensitively).
// A missing directory yields an empty list.
func (s Store) List() ([]Query, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Query
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), Ext) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if ValidateName(name) != nil {
			continue
		}
		q := Query{Name: name, Path: filepath.Join(s.Dir, e.Name())}
		if info, err := e.Info(); err == nil {
			q.Modified = info.ModTime()
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Load returns the SQL of the named query.
func (s Store) Load(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	b, err := os.ReadFile(s.path(name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Exists reports whether a query of that name is saved.
func (s Store) Exists(name string) bool {
	if ValidateName(name) != nil {
		return false
	}
	_, err := os.Stat(s.path(name))
	return err == nil
}

// Save writes the query, creating the directory if needed. Unless
// overwrite is set, an existing query of the same name is not replaced and
// ErrExists is returned.
func (s Store) Save(name, sql string, overwrite bool) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if strings.TrimSpace(sql) == "" {
		return errors.New("the query is empty")
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	p := s.path(name)
	if !overwrite && s.Exists(name) {
		return ErrExists
	}
	if !strings.HasSuffix(sql, "\n") {
		sql += "\n"
	}
	tmp, err := os.CreateTemp(s.Dir, ".tmp-*"+Ext)
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(sql); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Filter returns the queries whose names contain substr, ignoring case.
func Filter(list []Query, substr string) []Query {
	substr = strings.ToLower(strings.TrimSpace(substr))
	if substr == "" {
		return list
	}
	var out []Query
	for _, q := range list {
		if strings.Contains(strings.ToLower(q.Name), substr) {
			out = append(out, q)
		}
	}
	return out
}
