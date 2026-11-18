package main

import (
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestStartWithUs_ChildOutlivesOtherThreads (MI5's cameras): a child that
// startWithUs started dies with uploadspike only. Linux sends its
// Pdeathsig when the thread that created it ends, and Go ends a thread
// (other than the main one) whenever a goroutine exits locked to it, as
// hubproc's own goroutine does at each hub's end; the lab run on 6415e55
// and a local run of the lab's timing lost reccontroller when the login's
// hub stopped, since the creating thread, once unlocked, served that
// goroutine. Here the child is started off the main thread, the starting
// goroutine leaves its thread, and goroutines lock threads and exit,
// that one included; the child lives on, and ends with its kill.
func TestStartWithUs_ChildOutlivesOtherThreads(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	type startResult struct {
		done <-chan struct{}
		err  error
		hits int
	}
	res := make(chan startResult, 1)
	go func() {
		runtime.LockOSThread() // holds this thread (the main one, maybe), so the next goroutine runs on another
		defer runtime.UnlockOSThread()
		inner := make(chan startResult, 1)
		go func() {
			runtime.LockOSThread()
			tid := syscall.Gettid()
			done, err := startWithUs(cmd)
			runtime.UnlockOSThread() // the starting thread goes back to the pool
			hits := 0
			var mu sync.Mutex
			for range 50 {
				var wg sync.WaitGroup
				for range runtime.GOMAXPROCS(0) * 4 {
					wg.Go(func() {
						runtime.LockOSThread() // never unlocked: the thread ends with the goroutine
						if syscall.Gettid() == tid {
							mu.Lock()
							hits++
							mu.Unlock()
						}
						time.Sleep(time.Millisecond)
					})
				}
				wg.Wait()
			}
			inner <- startResult{done, err, hits}
		}()
		res <- <-inner
	}()
	r := <-res
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-r.done })
	time.Sleep(200 * time.Millisecond)
	select {
	case <-r.done:
		t.Fatalf("the child died with another goroutine's thread (%d goroutines ran on the starting thread)", r.hits)
	default:
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the child is gone: %v", err)
	}
}
