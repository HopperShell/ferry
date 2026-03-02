package fs

import "context"

// SkipDirs is the default set of directory names that are skipped during
// recursive walks and transfers. These are typically large generated or
// dependency directories that users rarely want to transfer.
var SkipDirs = map[string]bool{
	".venv":          true,
	"node_modules":   true,
	"__pycache__":    true,
	".git":           true,
	".svn":           true,
	".hg":            true,
	".tox":           true,
	".mypy_cache":    true,
	".pytest_cache":  true,
	".next":          true,
	".nuxt":          true,
	".cache":         true,
	".terraform":     true,
}

// WalkResult represents a single entry found during recursive directory walking.
type WalkResult struct {
	Entry Entry
	Dir   string // the directory containing this entry
}

const (
	walkMaxDepth = 15
	walkMaxFiles = 10000
)

// Walk recursively lists entries starting from root, streaming results into the
// provided channel. It uses the FileSystem.List method (no interface changes
// needed) and respects context cancellation for early exit. The channel is
// closed when the walk completes or is cancelled.
func Walk(ctx context.Context, filesystem FileSystem, root string, results chan<- WalkResult) {
	defer close(results)
	walkDir(ctx, filesystem, root, 0, results, new(int))
}

func walkDir(ctx context.Context, filesystem FileSystem, dir string, depth int, results chan<- WalkResult, count *int) {
	if depth > walkMaxDepth || *count >= walkMaxFiles {
		return
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	entries, err := filesystem.List(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if *count >= walkMaxFiles {
			return
		}
		select {
		case <-ctx.Done():
			return
		case results <- WalkResult{Entry: e, Dir: dir}:
			*count++
		}
		if e.IsDir && !SkipDirs[e.Name] {
			walkDir(ctx, filesystem, e.Path, depth+1, results, count)
		}
	}
}
