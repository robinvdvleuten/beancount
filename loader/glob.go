package loader

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// hasGlobMeta reports whether an include path is a glob pattern.
func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

// globInclude expands an include pattern the way beancount's loader does with
// Python's glob.glob(pattern, recursive=True): "**" as a whole path segment
// matches zero or more directories, and wildcards skip dotfiles unless the
// segment itself starts with a dot. A relative pattern is matched from baseDir,
// so metacharacters in baseDir itself are never interpreted. Matches are sorted
// for a deterministic load order.
func globInclude(baseDir, pattern string) ([]string, error) {
	root := baseDir
	if filepath.IsAbs(pattern) {
		root = filepath.VolumeName(pattern) + string(filepath.Separator)
		pattern = pattern[len(filepath.VolumeName(pattern)):]
	}

	var segments []string
	for _, segment := range strings.Split(filepath.ToSlash(pattern), "/") {
		if segment == "" || segment == "." {
			continue
		}
		// Surface a malformed segment up front; Match only reports it while matching.
		if _, err := filepath.Match(segment, ""); err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}

	var matches []string
	globSegments(root, segments, &matches)
	slices.Sort(matches)
	return slices.Compact(matches), nil
}

func globSegments(dir string, segments []string, matches *[]string) {
	if len(segments) == 0 {
		*matches = append(*matches, dir)
		return
	}
	segment, rest := segments[0], segments[1:]

	switch {
	case segment == "**":
		globSegments(dir, rest, matches)
		for _, entry := range readDir(dir) {
			// Symlinked directories are not followed, which rules out cycles.
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				globSegments(filepath.Join(dir, entry.Name()), segments, matches)
			}
		}
	case !hasGlobMeta(segment):
		path := filepath.Join(dir, segment)
		if _, err := os.Lstat(path); err == nil {
			globSegments(path, rest, matches)
		}
	default:
		for _, entry := range readDir(dir) {
			name := entry.Name()
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(segment, ".") {
				continue
			}
			if ok, _ := filepath.Match(segment, name); ok {
				globSegments(filepath.Join(dir, name), rest, matches)
			}
		}
	}
}

// readDir lists dir, treating unreadable or non-directory paths as empty like
// Python's glob does.
func readDir(dir string) []os.DirEntry {
	entries, _ := os.ReadDir(dir)
	return entries
}
