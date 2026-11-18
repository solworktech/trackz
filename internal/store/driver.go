package store

import (
	"database/sql/driver"
	"fmt"
	"sync/atomic"

	"modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/solworktech/trackz/internal/titles"
)

const driverName = "sqlite"

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
}
