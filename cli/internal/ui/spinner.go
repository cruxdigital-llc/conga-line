package ui

import (
	"fmt"
	"sync"
	"time"
)

type Spinner struct {
	msg  string
	done chan struct{}
	wg   sync.WaitGroup
}

func NewSpinner(msg string) *Spinner {
	s := &Spinner{
		msg:  msg,
		done: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer s.wg.Done()
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			fmt.Printf("\r\033[K")
			return
		case <-ticker.C:
			fmt.Printf("\r%s %s", frames[i%len(frames)], s.msg)
			i++
		}
	}
}

func (s *Spinner) Stop() {
	close(s.done)
	s.wg.Wait()
}
