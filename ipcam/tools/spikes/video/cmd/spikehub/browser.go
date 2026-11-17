package main

// openBrowserFunc opens url in the default browser and returns without waiting
// for it. On the owner's Windows PC spikehub opens its own page at start, so
// that the owner only presses «Rulează tot pentru acest dispozitiv»
// (post-release task A); -browser=false turns that off.
type openBrowserFunc func(url string) error
