package loader

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// hasGlobMeta reports whether a path segment is a glob pattern.
func hasGlobMeta(segment string) bool {
	return strings.ContainsAny(segment, "*?[")
}

// globInclude expands an include pattern the way beancount's loader does with
// Python's glob.glob(pattern, recursive=True): "**" as a whole path segment
// matches zero or more directories, and wildcards skip dotfiles unless the
// segment itself starts with a dot. A relative pattern is matched from baseDir,
// so metacharacters in baseDir itself are never interpreted. A segment is
// matched as Python's fnmatch matches it (fnmatch). Matches are sorted for a
// deterministic load order.
func globInclude(baseDir, pattern string) []string {
	root := baseDir
	if filepath.IsAbs(pattern) {
		root = filepath.VolumeName(pattern) + string(filepath.Separator)
		pattern = pattern[len(filepath.VolumeName(pattern)):]
	}

	var segments []globSegment
	for _, segment := range strings.Split(filepath.ToSlash(pattern), "/") {
		if segment == "" || segment == "." {
			continue
		}
		var match func(string) bool
		if segment != "**" && hasGlobMeta(segment) {
			match = fnmatch(segment)
		}
		segments = append(segments, globSegment{text: segment, match: match})
	}

	var matches []string
	globSegments(root, segments, &matches)
	slices.Sort(matches)
	return slices.Compact(matches)
}

// globSegment is one segment of an include pattern, with the matcher a
// wildcard segment is compiled to once, however many directories it is
// matched in.
type globSegment struct {
	text  string
	match func(name string) bool
}

func globSegments(dir string, segments []globSegment, matches *[]string) {
	if len(segments) == 0 {
		*matches = append(*matches, dir)
		return
	}
	segment, rest := segments[0], segments[1:]

	switch {
	case segment.text == "**":
		globSegments(dir, rest, matches)
		for _, entry := range readDir(dir) {
			// Symlinked directories are not followed, which rules out cycles.
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				globSegments(filepath.Join(dir, entry.Name()), segments, matches)
			}
		}
	case segment.match == nil:
		path := filepath.Join(dir, segment.text)
		if _, err := os.Lstat(path); err == nil {
			globSegments(path, rest, matches)
		}
	default:
		for _, entry := range readDir(dir) {
			name := entry.Name()
			if strings.HasPrefix(name, ".") && !strings.HasPrefix(segment.text, ".") {
				continue
			}
			if segment.match(name) {
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
