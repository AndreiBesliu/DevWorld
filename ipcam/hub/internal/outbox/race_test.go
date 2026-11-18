//go:build race

package outbox

// raceEnabled is true in a test binary built with -race.
const raceEnabled = true
