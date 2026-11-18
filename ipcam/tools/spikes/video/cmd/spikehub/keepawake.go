package main

// keepAwakeFunc asks the OS to keep the PC from sleeping while spikehub serves,
// and returns the function that withdraws the request (R49). The phone part of
// the run takes about 1.5 h with nobody at the PC, and Windows counts only
// keyboard and mouse input, not network traffic, as activity: without the
// request, the PC sleeps in the middle of a scenario and the hub is gone. The
// display may still turn off.
type keepAwakeFunc func() (release func(), err error)
