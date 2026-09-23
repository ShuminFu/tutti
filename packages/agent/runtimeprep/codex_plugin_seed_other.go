//go:build !darwin

package runtimeprep

// Non-darwin platforms have no cheap whole-directory COW clone we can rely on
// (a plain copy would cost the same 89MB we are trying to save), so seeding is
// disabled and codex keeps downloading the marketplace itself, as before.
func cloneDirCOW(src, dst string) error {
	return errCodexPluginSeedUnsupported
}

func tryLockCodexPluginsSync(string) (func(), error) {
	return nil, errCodexPluginSeedUnsupported
}
