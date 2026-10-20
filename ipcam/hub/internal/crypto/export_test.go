package crypto

// SetResealCutHook makes Reseal call hook after each truncation and sync of
// the file, where a cut could stop it, and returns a function that removes it.
func SetResealCutHook(hook func()) (restore func()) {
	previous := resealCutHook
	resealCutHook = hook
	return func() { resealCutHook = previous }
}
