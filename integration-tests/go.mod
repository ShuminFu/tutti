module github.com/tutti-os/tutti/integration-tests

go 1.24.3

toolchain go1.24.5

require (
	github.com/tutti-os/tutti/packages/agent/daemon v0.0.0
	github.com/tutti-os/tutti/packages/agent/store-sqlite v0.0.0
	github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical v0.0.0
	github.com/tutti-os/tutti/services/tuttid v0.0.0
)

replace github.com/tutti-os/tutti/packages/agent/daemon => ../packages/agent/daemon

replace github.com/tutti-os/tutti/packages/agent/store-sqlite => ../packages/agent/store-sqlite

replace github.com/tutti-os/tutti/packages/agent/store-sqlite/canonical => ../packages/agent/store-sqlite/canonical

replace github.com/tutti-os/tutti/services/tuttid => ../services/tuttid
