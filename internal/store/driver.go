package store

import (
	"database/sql/driver"
	"fmt"
	"sync/atomic"

	"modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/solworktech/trackz/internal/titles"
)

const driverName = "sqlite"

// taskGrouper backs the task_group() SQL function: it returns the task
// group of a window (process name, recorded title), "" for none.
var taskGrouper atomic.Pointer[func(process, title string) string]

// SetTaskGrouper sets the function task_group() applies (none: every
// window is in no group).
func SetTaskGrouper(f func(process, title string) string) { taskGrouper.Store(&f) }

// titleNormalizer backs the norm_title() SQL function.
var titleNormalizer atomic.Pointer[titles.Normalizer]

// SetTitleNormalizer sets the rules norm_title() applies (by default
// titles.DefaultRules).
func SetTitleNormalizer(n *titles.Normalizer) { titleNormalizer.Store(n) }

func init() {
	titleNormalizer.Store(titles.Default())
	// norm_title(window_name) lets custom queries group titles the way the
	// predefined views do, e.g. GROUP BY norm_title(window_name).
	sqlite.MustRegisterDeterministicScalarFunction("norm_title", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			switch v := args[0].(type) {
			case nil:
				return nil, nil
			case string:
				return titleNormalizer.Load().Normalize(v), nil
			case []byte:
				return titleNormalizer.Load().Normalize(string(v)), nil
			default:
				return nil, fmt.Errorf("norm_title: expected text, got %T", v)
			}
		})
	// task_group(process_name, window_name) gives custom queries the task
	// groups of the Task Groups view (NULL for windows in none), e.g.
	// GROUP BY task_group(process_name, window_name).
	sqlite.MustRegisterDeterministicScalarFunction("task_group", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			var s [2]string
			for i, a := range args {
				switch v := a.(type) {
				case nil:
					return nil, nil
				case string:
					s[i] = v
				case []byte:
					s[i] = string(v)
				default:
					return nil, fmt.Errorf("task_group: expected text, got %T", v)
				}
			}
			f := taskGrouper.Load()
			if f == nil {
				return nil, nil
			}
			if g := (*f)(s[0], s[1]); g != "" {
				return g, nil
			}
			return nil, nil
		})
}
