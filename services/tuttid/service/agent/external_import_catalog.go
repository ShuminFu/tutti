package agent

import (
	"context"

	"github.com/tutti-os/tutti/services/tuttid/data/externalimportcatalog"
)

// ExternalImportCatalog is the derived transcript index used by scan and
// import. Implementations live in services/tuttid/data/externalimportcatalog
// and are injected at the composition root. A nil catalog degrades to a live
// summary scan.
type ExternalImportCatalog interface {
	Lookup(ctx context.Context, keys []externalimportcatalog.Key) (map[externalimportcatalog.Key]externalimportcatalog.Entry, error)
	Upsert(ctx context.Context, entries []externalimportcatalog.Entry) error
	CompleteRoot(ctx context.Context, provider, root string, generation int64, live []externalimportcatalog.Key) error
	Close() error
}
